"""
数据集加载与预处理

从 Go 端导出的 features.csv 文件中加载数据，
构建 PyTorch Dataset 用于 Transformer 模型训练。

features.csv 格式（多日版）：
    date, symbol, time, f0, f1, ..., fN, label, price_chg, phase

Go 端的特征工程已将每个样本展平为一维向量，
这里需要将其重塑为 (window_size, per_bar_features) 的二维序列，
以匹配 Transformer 的输入格式。

多日数据拆分策略：
- 按日期排序后，前 N-2 天 -> 训练集
- 倒数第 2 天 -> 验证集
- 最后 1 天 -> 测试集
- 若只有 1 天数据，退化为按比例拆分
"""

import glob
import os
from typing import List, Optional, Tuple

import numpy as np
import pandas as pd
import torch
from sklearn.preprocessing import StandardScaler
from torch.utils.data import DataLoader, Dataset


class TickFeatureDataset(Dataset):
    """Tick 级特征数据集"""

    def __init__(
        self,
        features: np.ndarray,
        labels: np.ndarray,
        price_chgs: np.ndarray,
        window_size: int,
    ):
        """
        Args:
            features: (N, total_feature_dim) 展平的特征矩阵
            labels: (N,) 标签数组 (0=跌, 1=涨)
            price_chgs: (N,) 价格变化百分比
            window_size: 窗口大小（Go 端的 window_size）
        """
        self.labels = torch.LongTensor(labels)
        self.price_chgs = torch.FloatTensor(price_chgs)
        self.window_size = window_size

        # 计算每根 bar 的特征维度
        total_dim = features.shape[1]
        # Go 端: 14 features/bar * window_size + 10 窗口统计
        self.bar_features = 14
        self.window_stats = total_dim - self.bar_features * window_size

        # 分离 bar 级特征和窗口级特征
        bar_part = features[:, : self.bar_features * window_size]
        stat_part = features[:, self.bar_features * window_size :]

        # 重塑 bar 特征为序列形式 (N, window_size, bar_features)
        bar_seq = bar_part.reshape(-1, window_size, self.bar_features)

        # 将窗口统计特征广播到每个时间步 (N, window_size, window_stats)
        stat_broadcast = np.tile(
            stat_part[:, np.newaxis, :], (1, window_size, 1)
        )

        # 拼接: (N, window_size, bar_features + window_stats)
        full_seq = np.concatenate([bar_seq, stat_broadcast], axis=2)
        self.features = torch.FloatTensor(full_seq)

        self.seq_feature_dim = self.bar_features + self.window_stats

    def __len__(self):
        return len(self.labels)

    def __getitem__(self, idx):
        return {
            "features": self.features[idx],     # (window_size, feature_dim)
            "label": self.labels[idx],           # scalar
            "price_chg": self.price_chgs[idx],   # scalar
        }


def _extract_features_labels(df: pd.DataFrame):
    """从 DataFrame 中提取特征矩阵、标签、价格变化"""
    feature_cols = [c for c in df.columns if c.startswith("f")]
    features = df[feature_cols].values.astype(np.float32)
    labels = df["label"].values.astype(np.int64)
    price_chgs = df["price_chg"].values.astype(np.float32)
    # 处理 NaN / Inf
    features = np.nan_to_num(features, nan=0.0, posinf=0.0, neginf=0.0)
    return features, labels, price_chgs


def load_features_csv(
    csv_path: str,
    window_size: int = 10,
    normalize: bool = True,
    train_ratio: float = 0.7,
    val_ratio: float = 0.15,
) -> Tuple[TickFeatureDataset, TickFeatureDataset, TickFeatureDataset, Optional[StandardScaler]]:
    """
    从单个 CSV 加载特征数据并拆分为训练/验证/测试集。
    如果 CSV 中包含 date 列且有多日数据，自动按日期拆分；
    否则按时间顺序比例拆分。

    Returns:
        (train_dataset, val_dataset, test_dataset, scaler)
    """
    df = pd.read_csv(csv_path)
    return _split_and_build(df, window_size, normalize, train_ratio, val_ratio)


def load_features_dir(
    data_dir: str,
    window_size: int = 10,
    normalize: bool = True,
    train_ratio: float = 0.7,
    val_ratio: float = 0.15,
) -> Tuple[TickFeatureDataset, TickFeatureDataset, TickFeatureDataset, Optional[StandardScaler]]:
    """
    从目录下加载多个特征 CSV，合并后按日期拆分。

    Returns:
        (train_dataset, val_dataset, test_dataset, scaler)
    """
    csv_files = sorted(glob.glob(os.path.join(data_dir, "*.csv")))
    if not csv_files:
        raise FileNotFoundError(f"No CSV files in {data_dir}")

    dfs = [pd.read_csv(f) for f in csv_files]
    df = pd.concat(dfs, ignore_index=True)
    print(f"[dataset] loaded {len(csv_files)} CSV files, total {len(df)} samples")

    return _split_and_build(df, window_size, normalize, train_ratio, val_ratio)


def _split_and_build(
    df: pd.DataFrame,
    window_size: int,
    normalize: bool,
    train_ratio: float,
    val_ratio: float,
) -> Tuple[TickFeatureDataset, TickFeatureDataset, TickFeatureDataset, Optional[StandardScaler]]:
    """根据 date 列智能拆分数据，构建 Dataset"""

    features, labels, price_chgs = _extract_features_labels(df)

    # 判断是否有多日数据
    has_date = "date" in df.columns and df["date"].nunique() > 1
    if has_date:
        dates = sorted(df["date"].unique())
        n_days = len(dates)
        print(f"[dataset] multi-day data: {n_days} days -> {dates}")

        if n_days >= 3:
            # 前 N-2 天训练，倒数第 2 天验证，最后 1 天测试
            train_dates = set(dates[:-2])
            val_dates = {dates[-2]}
            test_dates = {dates[-1]}
        elif n_days == 2:
            # 第 1 天训练，第 2 天各半验证+测试
            train_dates = {dates[0]}
            val_dates = set()
            test_dates = {dates[1]}
        else:
            train_dates = set(dates)
            val_dates = set()
            test_dates = set()

        train_mask = df["date"].isin(train_dates).values
        val_mask = df["date"].isin(val_dates).values
        test_mask = df["date"].isin(test_dates).values

        # 如果验证集为空（2天场景），从测试集前半部分取
        if val_mask.sum() == 0 and test_mask.sum() > 0:
            test_indices = np.where(test_mask)[0]
            split_point = len(test_indices) // 2
            val_indices = test_indices[:split_point]
            test_indices_new = test_indices[split_point:]
            val_mask = np.zeros(len(df), dtype=bool)
            val_mask[val_indices] = True
            test_mask = np.zeros(len(df), dtype=bool)
            test_mask[test_indices_new] = True

        print(f"[dataset] split: train={train_mask.sum()}, val={val_mask.sum()}, test={test_mask.sum()}")
    else:
        # 按比例顺序拆分
        n = len(features)
        train_end = int(n * train_ratio)
        val_end = int(n * (train_ratio + val_ratio))
        train_mask = np.zeros(n, dtype=bool)
        train_mask[:train_end] = True
        val_mask = np.zeros(n, dtype=bool)
        val_mask[train_end:val_end] = True
        test_mask = np.zeros(n, dtype=bool)
        test_mask[val_end:] = True

    # 标准化（仅在训练集上 fit）
    scaler = None
    if normalize:
        scaler = StandardScaler()
        features[train_mask] = scaler.fit_transform(features[train_mask])
        if val_mask.sum() > 0:
            features[val_mask] = scaler.transform(features[val_mask])
        if test_mask.sum() > 0:
            features[test_mask] = scaler.transform(features[test_mask])

    train_ds = TickFeatureDataset(
        features[train_mask], labels[train_mask],
        price_chgs[train_mask], window_size
    )
    val_ds = TickFeatureDataset(
        features[val_mask], labels[val_mask],
        price_chgs[val_mask], window_size
    ) if val_mask.sum() > 0 else TickFeatureDataset(
        features[train_mask][-10:], labels[train_mask][-10:],
        price_chgs[train_mask][-10:], window_size
    )
    test_ds = TickFeatureDataset(
        features[test_mask], labels[test_mask],
        price_chgs[test_mask], window_size
    ) if test_mask.sum() > 0 else TickFeatureDataset(
        features[train_mask][-10:], labels[train_mask][-10:],
        price_chgs[train_mask][-10:], window_size
    )

    print(f"[dataset] loaded {len(features)} samples: train={len(train_ds)}, "
          f"val={len(val_ds)}, test={len(test_ds)}")
    print(f"[dataset] feature shape per sample: ({window_size}, {train_ds.seq_feature_dim})")
    print(f"[dataset] label distribution: 0(fall)={np.sum(labels==0)}, 1(rise)={np.sum(labels==1)}")

    return train_ds, val_ds, test_ds, scaler


def create_dataloaders(
    train_ds: TickFeatureDataset,
    val_ds: TickFeatureDataset,
    test_ds: TickFeatureDataset,
    batch_size: int = 32,
) -> Tuple[DataLoader, DataLoader, DataLoader]:
    """创建 DataLoader"""
    train_loader = DataLoader(train_ds, batch_size=batch_size, shuffle=True, drop_last=True)
    val_loader = DataLoader(val_ds, batch_size=batch_size, shuffle=False)
    test_loader = DataLoader(test_ds, batch_size=batch_size, shuffle=False)
    return train_loader, val_loader, test_loader