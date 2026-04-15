"""
Tick-level Transformer 模型

用于学习 tick 级交易数据中的涨跌模式：
- 输入：一个时间窗口内聚合 bar 的特征序列 (batch, seq_len, feature_dim)
- 输出：未来价格涨跌概率 (batch, num_classes)

核心设计：
1. 特征投影层：将原始特征映射到 d_model 维空间
2. 位置编码：注入时间序列位置信息
3. Transformer Encoder：捕获序列内的时序依赖和交易模式
4. 分类头：输出涨跌概率
"""

import math
import torch
import torch.nn as nn


class PositionalEncoding(nn.Module):
    """正弦-余弦位置编码"""

    def __init__(self, d_model: int, max_len: int = 500, dropout: float = 0.1):
        super().__init__()
        self.dropout = nn.Dropout(p=dropout)

        pe = torch.zeros(max_len, d_model)
        position = torch.arange(0, max_len, dtype=torch.float).unsqueeze(1)
        div_term = torch.exp(
            torch.arange(0, d_model, 2).float() * (-math.log(10000.0) / d_model)
        )
        pe[:, 0::2] = torch.sin(position * div_term)
        pe[:, 1::2] = torch.cos(position * div_term)
        pe = pe.unsqueeze(0)  # (1, max_len, d_model)
        self.register_buffer("pe", pe)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        """x: (batch, seq_len, d_model)"""
        x = x + self.pe[:, : x.size(1), :]
        return self.dropout(x)


class TickTransformer(nn.Module):
    """
    Tick 级 Transformer 分类模型

    架构：
        Input (batch, window_size, raw_feature_dim)
          -> Linear projection (batch, window_size, d_model)
          -> Positional Encoding
          -> Transformer Encoder × N layers
          -> Global Average Pooling
          -> Classification Head -> (batch, num_classes)
    """

    def __init__(
        self,
        feature_dim: int,
        d_model: int = 64,
        nhead: int = 4,
        num_layers: int = 3,
        dim_feedforward: int = 128,
        dropout: float = 0.1,
        num_classes: int = 2,
    ):
        super().__init__()

        self.feature_dim = feature_dim
        self.d_model = d_model

        # 特征投影：将每根 bar 的原始特征映射到 d_model
        self.input_projection = nn.Sequential(
            nn.Linear(feature_dim, d_model),
            nn.LayerNorm(d_model),
            nn.ReLU(),
            nn.Dropout(dropout),
        )

        # 位置编码
        self.pos_encoder = PositionalEncoding(d_model, dropout=dropout)

        # Transformer Encoder
        encoder_layer = nn.TransformerEncoderLayer(
            d_model=d_model,
            nhead=nhead,
            dim_feedforward=dim_feedforward,
            dropout=dropout,
            batch_first=True,
            activation="gelu",
        )
        self.transformer_encoder = nn.TransformerEncoder(
            encoder_layer, num_layers=num_layers
        )

        # 分类头
        self.classifier = nn.Sequential(
            nn.Linear(d_model, d_model // 2),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(d_model // 2, num_classes),
        )

        # 回归头：预测价格变化幅度
        self.regressor = nn.Sequential(
            nn.Linear(d_model, d_model // 2),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(d_model // 2, 1),
        )

        self._init_weights()

    def _init_weights(self):
        """Xavier 初始化"""
        for p in self.parameters():
            if p.dim() > 1:
                nn.init.xavier_uniform_(p)

    def forward(
        self, x: torch.Tensor, mask: torch.Tensor = None
    ) -> dict:
        """
        Args:
            x: (batch, seq_len, feature_dim) 输入特征序列
            mask: (batch, seq_len) 可选的注意力掩码

        Returns:
            dict with:
                - logits: (batch, num_classes) 分类 logits
                - probs: (batch, num_classes) 概率分布
                - price_chg: (batch, 1) 预测价格变化
                - attention_weights: encoder 的注意力权重（用于可视化）
        """
        # 投影到 d_model 空间
        x = self.input_projection(x)  # (batch, seq_len, d_model)

        # 位置编码
        x = self.pos_encoder(x)

        # Transformer Encoder
        if mask is not None:
            x = self.transformer_encoder(x, src_key_padding_mask=mask)
        else:
            x = self.transformer_encoder(x)

        # Global Average Pooling：对序列维度取平均
        x_pooled = x.mean(dim=1)  # (batch, d_model)

        # 分类输出
        logits = self.classifier(x_pooled)  # (batch, num_classes)
        probs = torch.softmax(logits, dim=-1)

        # 回归输出
        price_chg = self.regressor(x_pooled)  # (batch, 1)

        return {
            "logits": logits,
            "probs": probs,
            "price_chg": price_chg.squeeze(-1),
        }

    def predict(self, x: torch.Tensor) -> dict:
        """推理模式"""
        self.eval()
        with torch.no_grad():
            output = self.forward(x)
            pred_class = output["probs"].argmax(dim=-1)
            confidence = output["probs"].max(dim=-1).values
            return {
                "class": pred_class,  # 0=跌, 1=涨
                "confidence": confidence,
                "rise_prob": output["probs"][:, 1],
                "fall_prob": output["probs"][:, 0],
                "price_chg": output["price_chg"],
            }