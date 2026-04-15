#!/usr/bin/env python3
"""
模型推理 HTTP 服务

提供 REST API 供 Go 后端调用：
    GET  /health       -> 健康检查
    POST /predict      -> 批量预测
    GET  /model-info   -> 模型信息

启动：
    python ml/scripts/serve.py --model data/models/best_model.pt --port 5000
"""

import argparse
import json
import os
import sys

import numpy as np
import torch
from flask import Flask, request, jsonify

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from models.transformer import TickTransformer

app = Flask(__name__)

# 全局模型和配置
model = None
scaler = None
device = None
model_config = {}


def load_model(model_path: str, config: dict):
    """加载训练好的模型"""
    global model, scaler, device, model_config
    model_config = config

    device = torch.device(
        "cuda" if torch.cuda.is_available()
        else "mps" if torch.backends.mps.is_available()
        else "cpu"
    )

    feature_dim = config.get("feature_dim", 24)
    model = TickTransformer(
        feature_dim=feature_dim,
        d_model=config.get("d_model", 64),
        nhead=config.get("nhead", 4),
        num_layers=config.get("num_layers", 3),
        dim_feedforward=config.get("dim_feedforward", 128),
        dropout=0.0,  # 推理时不 dropout
        num_classes=2,
    ).to(device)

    if os.path.exists(model_path):
        state_dict = torch.load(model_path, map_location=device, weights_only=True)
        model.load_state_dict(state_dict)
        print(f"[serve] model loaded from {model_path}")
    else:
        print(f"[serve] WARNING: model file not found: {model_path}, using random weights")

    model.eval()

    # 加载 scaler
    scaler_path = os.path.join(os.path.dirname(model_path), "scaler.pkl")
    if os.path.exists(scaler_path):
        import joblib
        scaler = joblib.load(scaler_path)
        print(f"[serve] scaler loaded from {scaler_path}")


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok", "model_loaded": model is not None})


@app.route("/model-info", methods=["GET"])
def model_info():
    if model is None:
        return jsonify({"error": "model not loaded"}), 400
    total_params = sum(p.numel() for p in model.parameters())
    return jsonify({
        "total_params": total_params,
        "device": str(device),
        "config": model_config,
    })


@app.route("/predict", methods=["POST"])
def predict():
    """
    批量预测接口

    请求体 JSON:
    {
        "features": [[...], [...], ...],  # (N, total_feature_dim) 展平特征
        "window_size": 10
    }

    响应:
    [
        {"rise_prob": 0.72, "fall_prob": 0.28, "action": "buy", "confidence": 0.72, ...},
        ...
    ]
    """
    if model is None:
        return jsonify({"error": "model not loaded"}), 400

    data = request.get_json()
    if not data or "features" not in data:
        return jsonify({"error": "missing 'features' in request body"}), 400

    features = np.array(data["features"], dtype=np.float32)
    window_size = data.get("window_size", 10)

    # 标准化
    if scaler is not None:
        features = scaler.transform(features)

    # 重塑为序列格式
    bar_features = 14
    window_stats = features.shape[1] - bar_features * window_size

    bar_part = features[:, :bar_features * window_size]
    stat_part = features[:, bar_features * window_size:]

    bar_seq = bar_part.reshape(-1, window_size, bar_features)
    stat_broadcast = np.tile(stat_part[:, np.newaxis, :], (1, window_size, 1))
    full_seq = np.concatenate([bar_seq, stat_broadcast], axis=2)

    x = torch.FloatTensor(full_seq).to(device)

    # 推理
    result = model.predict(x)

    # 构建响应
    predictions = []
    conf_threshold = model_config.get("confidence_threshold", 0.6)
    for i in range(len(features)):
        rise_prob = float(result["rise_prob"][i])
        fall_prob = float(result["fall_prob"][i])
        confidence = float(result["confidence"][i])
        price_chg = float(result["price_chg"][i])

        if confidence >= conf_threshold:
            action = "buy" if rise_prob > fall_prob else "sell"
        else:
            action = "hold"

        predictions.append({
            "rise_prob": round(rise_prob, 4),
            "fall_prob": round(fall_prob, 4),
            "action": action,
            "confidence": round(confidence, 4),
            "expected_pnl": round(price_chg, 4),
        })

    return jsonify(predictions)


def main():
    parser = argparse.ArgumentParser(description="Model Inference Service")
    parser.add_argument("--model", type=str, default="data/models/best_model.pt")
    parser.add_argument("--port", type=int, default=5000)
    parser.add_argument("--d-model", type=int, default=64)
    parser.add_argument("--nhead", type=int, default=4)
    parser.add_argument("--num-layers", type=int, default=3)
    parser.add_argument("--feature-dim", type=int, default=24)
    args = parser.parse_args()

    config = {
        "d_model": args.d_model,
        "nhead": args.nhead,
        "num_layers": args.num_layers,
        "feature_dim": args.feature_dim,
        "dim_feedforward": args.d_model * 2,
        "confidence_threshold": 0.6,
    }

    load_model(args.model, config)
    print(f"[serve] starting on port {args.port}")
    app.run(host="0.0.0.0", port=args.port, debug=False)


if __name__ == "__main__":
    main()