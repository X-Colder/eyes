#!/usr/bin/env python3
"""
Transformer 模型训练脚本（支持单卡 / 多卡 DDP）

单卡用法：
    python ml/scripts/train.py --data data/features/features.csv

多卡用法（A800 八卡）：
    torchrun --nproc_per_node=8 ml/scripts/train.py --data data/features/features.csv
    torchrun --nproc_per_node=4 ml/scripts/train.py --data data/features/features.csv  # 指定4卡

流程：
    1. 加载 Go 端导出的特征 CSV
    2. 构建 Dataset & DataLoader（DDP 时自动分片）
    3. 初始化 Transformer 模型（DDP 包装）
    4. 训练（含 early stopping）
    5. 在测试集上评估（仅 rank 0）
    6. 保存最优模型和评估报告（仅 rank 0）
"""

import argparse
import json
import os
import sys
import time

import numpy as np
import torch
import torch.nn as nn
from torch.optim.lr_scheduler import StepLR

# DDP 相关
import torch.distributed as dist
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.utils.data.distributed import DistributedSampler

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from models.dataset import load_features_csv, load_features_dir, create_dataloaders
from models.transformer import TickTransformer


# ==================== DDP 工具函数 ====================

def is_ddp():
    """判断是否在 DDP 模式下运行"""
    return dist.is_available() and dist.is_initialized()


def get_rank():
    return dist.get_rank() if is_ddp() else 0


def get_world_size():
    return dist.get_world_size() if is_ddp() else 1


def is_main_process():
    return get_rank() == 0


def log(msg):
    """只在 rank 0 打印"""
    if is_main_process():
        print(msg, flush=True)


def setup_ddp():
    """初始化 DDP（由 torchrun 自动设置环境变量）"""
    if "RANK" not in os.environ:
        return False  # 非 DDP 模式

    dist.init_process_group(backend="nccl")
    local_rank = int(os.environ.get("LOCAL_RANK", 0))
    torch.cuda.set_device(local_rank)
    return True


def cleanup_ddp():
    if is_ddp():
        dist.destroy_process_group()


# ==================== 训练 / 评估 ====================

def parse_args():
    parser = argparse.ArgumentParser(description="Train Tick Transformer")
    parser.add_argument("--data", type=str, default="", help="features CSV path (single file)")
    parser.add_argument("--data-dir", type=str, default="", help="directory with multiple feature CSVs")
    parser.add_argument("--model-dir", type=str, default="data/models", help="model output dir")
    parser.add_argument("--window-size", type=int, default=10)
    parser.add_argument("--batch-size", type=int, default=32)
    parser.add_argument("--epochs", type=int, default=100)
    parser.add_argument("--lr", type=float, default=0.001)
    parser.add_argument("--d-model", type=int, default=64)
    parser.add_argument("--nhead", type=int, default=4)
    parser.add_argument("--num-layers", type=int, default=3)
    parser.add_argument("--patience", type=int, default=15, help="early stopping patience")
    return parser.parse_args()


def train_epoch(model, loader, criterion_cls, criterion_reg, optimizer, device, sampler=None, epoch=0):
    model.train()
    if sampler is not None:
        sampler.set_epoch(epoch)  # DDP: 每个 epoch 打乱不同

    total_loss = 0.0
    correct = 0
    total = 0

    for batch in loader:
        features = batch["features"].to(device, non_blocking=True)
        labels = batch["label"].to(device, non_blocking=True)
        price_chgs = batch["price_chg"].to(device, non_blocking=True)

        optimizer.zero_grad()
        output = model(features)

        loss_cls = criterion_cls(output["logits"], labels)
        loss_reg = criterion_reg(output["price_chg"], price_chgs)
        loss = loss_cls + 0.1 * loss_reg

        loss.backward()
        torch.nn.utils.clip_grad_norm_(model.parameters(), max_norm=1.0)
        optimizer.step()

        total_loss += loss.item() * features.size(0)
        preds = output["logits"].argmax(dim=-1)
        correct += (preds == labels).sum().item()
        total += labels.size(0)

    # DDP: 聚合各卡指标
    if is_ddp():
        metrics = torch.tensor([total_loss, correct, total], dtype=torch.float64, device=device)
        dist.all_reduce(metrics, op=dist.ReduceOp.SUM)
        total_loss, correct, total = metrics.tolist()

    avg_loss = total_loss / total if total > 0 else 0
    accuracy = correct / total if total > 0 else 0
    return avg_loss, accuracy


def evaluate(model, loader, criterion_cls, criterion_reg, device):
    """评估（仅在 rank 0 上跑完整测试集）"""
    raw_model = model.module if isinstance(model, DDP) else model
    raw_model.eval()

    total_loss = 0.0
    correct = 0
    total = 0
    all_preds = []
    all_labels = []
    all_probs = []

    with torch.no_grad():
        for batch in loader:
            features = batch["features"].to(device, non_blocking=True)
            labels = batch["label"].to(device, non_blocking=True)
            price_chgs = batch["price_chg"].to(device, non_blocking=True)

            output = raw_model(features)
            loss_cls = criterion_cls(output["logits"], labels)
            loss_reg = criterion_reg(output["price_chg"], price_chgs)
            loss = loss_cls + 0.1 * loss_reg

            total_loss += loss.item() * features.size(0)
            preds = output["logits"].argmax(dim=-1)
            correct += (preds == labels).sum().item()
            total += labels.size(0)

            all_preds.extend(preds.cpu().numpy())
            all_labels.extend(labels.cpu().numpy())
            all_probs.extend(output["probs"].cpu().numpy())

    avg_loss = total_loss / total if total > 0 else 0
    accuracy = correct / total if total > 0 else 0
    return avg_loss, accuracy, np.array(all_preds), np.array(all_labels), np.array(all_probs)


def compute_metrics(preds, labels, probs):
    """计算交易相关指标"""
    from collections import Counter

    accuracy = np.mean(preds == labels)
    counter = Counter(zip(preds, labels))
    tp = counter.get((1, 1), 0)
    fp = counter.get((1, 0), 0)
    fn = counter.get((0, 1), 0)

    precision = tp / (tp + fp) if (tp + fp) > 0 else 0
    recall = tp / (tp + fn) if (tp + fn) > 0 else 0
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0

    high_conf_mask = np.max(probs, axis=1) > 0.6
    high_conf_acc = np.mean(preds[high_conf_mask] == labels[high_conf_mask]) if high_conf_mask.sum() > 0 else 0

    return {
        "accuracy": round(accuracy, 4),
        "precision": round(precision, 4),
        "recall": round(recall, 4),
        "f1": round(f1, 4),
        "high_conf_trade_ratio": round(float(high_conf_mask.mean()), 4),
        "high_conf_win_rate": round(float(high_conf_acc), 4),
        "total_samples": len(preds),
        "rise_predicted": int(np.sum(preds == 1)),
        "fall_predicted": int(np.sum(preds == 0)),
    }


# ==================== 主函数 ====================

def main():
    args = parse_args()

    # DDP 初始化
    use_ddp = setup_ddp()
    local_rank = int(os.environ.get("LOCAL_RANK", 0))
    world_size = get_world_size()

    if use_ddp:
        device = torch.device(f"cuda:{local_rank}")
    elif torch.cuda.is_available():
        device = torch.device("cuda")
    elif hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
        device = torch.device("mps")
    else:
        device = torch.device("cpu")

    log(f"[train] device: {device}, world_size: {world_size}, DDP: {use_ddp}")
    if use_ddp:
        log(f"[train] GPU per node: {torch.cuda.device_count()}")

    os.makedirs(args.model_dir, exist_ok=True)

    # ---- 加载数据 ----
    if args.data_dir:
        log(f"[train] loading data from directory {args.data_dir}")
        train_ds, val_ds, test_ds, scaler = load_features_dir(
            args.data_dir, window_size=args.window_size
        )
    elif args.data:
        log(f"[train] loading data from {args.data}")
        train_ds, val_ds, test_ds, scaler = load_features_csv(
            args.data, window_size=args.window_size
        )
    else:
        log("[train] ERROR: must specify --data or --data-dir")
        sys.exit(1)

    # DDP: 使用 DistributedSampler 分片数据
    train_sampler = DistributedSampler(train_ds, num_replicas=world_size, rank=get_rank(), shuffle=True) if use_ddp else None
    val_sampler = None  # 验证集不分片，每张卡看到完整集

    from torch.utils.data import DataLoader
    train_loader = DataLoader(
        train_ds, batch_size=args.batch_size,
        shuffle=(train_sampler is None), sampler=train_sampler,
        drop_last=True, num_workers=4, pin_memory=True,
    )
    val_loader = DataLoader(val_ds, batch_size=args.batch_size, shuffle=False, num_workers=2, pin_memory=True)
    test_loader = DataLoader(test_ds, batch_size=args.batch_size, shuffle=False, num_workers=2, pin_memory=True)

    # ---- 创建模型 ----
    feature_dim = train_ds.seq_feature_dim
    log(f"[train] creating model: feature_dim={feature_dim}, d_model={args.d_model}, "
        f"nhead={args.nhead}, layers={args.num_layers}")

    model = TickTransformer(
        feature_dim=feature_dim,
        d_model=args.d_model,
        nhead=args.nhead,
        num_layers=args.num_layers,
        dim_feedforward=args.d_model * 2,
        dropout=0.1,
        num_classes=2,
    ).to(device)

    if use_ddp:
        model = DDP(model, device_ids=[local_rank], output_device=local_rank)

    total_params = sum(p.numel() for p in model.parameters())
    log(f"[train] model parameters: {total_params:,}")

    # ---- 损失 & 优化器 ----
    label_counts = np.bincount(train_ds.labels.numpy(), minlength=2)
    weights = 1.0 / (label_counts + 1e-6)
    weights = weights / weights.sum()
    class_weights = torch.FloatTensor(weights).to(device)

    criterion_cls = nn.CrossEntropyLoss(weight=class_weights)
    criterion_reg = nn.MSELoss()

    # DDP: 学习率随卡数线性缩放
    effective_lr = args.lr * world_size
    optimizer = torch.optim.AdamW(model.parameters(), lr=effective_lr, weight_decay=1e-4)
    scheduler = StepLR(optimizer, step_size=20, gamma=0.5)

    log(f"[train] effective lr: {effective_lr} (base={args.lr} x {world_size} GPUs)")

    # ---- 训练循环 ----
    best_val_loss = float("inf")
    patience_counter = 0
    best_epoch = 0

    log(f"\n[train] starting training for {args.epochs} epochs...")
    start_time = time.time()

    for epoch in range(1, args.epochs + 1):
        train_loss, train_acc = train_epoch(
            model, train_loader, criterion_cls, criterion_reg,
            optimizer, device, sampler=train_sampler, epoch=epoch,
        )

        # 验证（每张卡都跑，但只 rank 0 打印）
        val_loss, val_acc, _, _, _ = evaluate(
            model, val_loader, criterion_cls, criterion_reg, device,
        )
        scheduler.step()

        lr = optimizer.param_groups[0]["lr"]
        log(f"  epoch {epoch:3d}/{args.epochs} | "
            f"train_loss={train_loss:.4f} train_acc={train_acc:.4f} | "
            f"val_loss={val_loss:.4f} val_acc={val_acc:.4f} | lr={lr:.6f}")

        # Early stopping（仅 rank 0 保存模型）
        if val_loss < best_val_loss:
            best_val_loss = val_loss
            patience_counter = 0
            best_epoch = epoch
            if is_main_process():
                raw_model = model.module if isinstance(model, DDP) else model
                torch.save(raw_model.state_dict(), os.path.join(args.model_dir, "best_model.pt"))
                log(f"  -> saved best model (val_loss={val_loss:.4f})")
        else:
            patience_counter += 1
            if patience_counter >= args.patience:
                log(f"  -> early stopping at epoch {epoch}")
                break

        # DDP: 同步 early stopping 决策
        if use_ddp:
            stop_flag = torch.tensor([1 if patience_counter >= args.patience else 0], device=device)
            dist.broadcast(stop_flag, src=0)
            if stop_flag.item() == 1:
                break

    elapsed = time.time() - start_time
    log(f"\n[train] training finished in {elapsed:.1f}s, best epoch: {best_epoch}")

    # ---- 测试集评估（仅 rank 0）----
    if is_main_process():
        raw_model = model.module if isinstance(model, DDP) else model
        raw_model.load_state_dict(
            torch.load(os.path.join(args.model_dir, "best_model.pt"), weights_only=True, map_location=device)
        )

        # 用原始模型（非 DDP）评估
        test_loss, test_acc, preds, labels, probs = evaluate(
            raw_model, test_loader, criterion_cls, criterion_reg, device,
        )
        metrics = compute_metrics(preds, labels, probs)

        print(f"\n[test] loss={test_loss:.4f}, accuracy={test_acc:.4f}")
        print(f"[test] metrics: {json.dumps(metrics, indent=2)}")

        report = {
            "test_loss": round(test_loss, 4),
            "test_accuracy": round(test_acc, 4),
            "metrics": metrics,
            "training_time_sec": round(elapsed, 1),
            "total_epochs": epoch,
            "best_epoch": best_epoch,
            "model_params": total_params,
            "world_size": world_size,
            "config": vars(args),
        }
        report_path = os.path.join(args.model_dir, "training_report.json")
        with open(report_path, "w") as f:
            json.dump(report, f, indent=2)
        print(f"[train] report saved to {report_path}")

        if scaler is not None:
            import joblib
            scaler_path = os.path.join(args.model_dir, "scaler.pkl")
            joblib.dump(scaler, scaler_path)
            print(f"[train] scaler saved to {scaler_path}")

    cleanup_ddp()


if __name__ == "__main__":
    main()