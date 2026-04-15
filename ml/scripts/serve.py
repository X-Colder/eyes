#!/usr/bin/env python3
"""
模型推理 HTTP 服务

提供 REST API：
    GET  /health           -> 健康检查
    GET  /model-info       -> 模型信息
    POST /predict          -> 批量预测（传入原始特征向量）
    POST /predict-csv      -> 基于特征 CSV 文件预测（更易用）

启动：
    python ml/scripts/serve.py --model data/models/best_model.pt --port 5000

请求示例：
    # 健康检查
    curl http://localhost:5000/health

    # 基于 CSV 文件推理（推荐）
    curl -X POST http://localhost:5000/predict-csv \
         -H "Content-Type: application/json" \
         -d '{"csv_path": "data/features/features.csv", "window_size": 10}'

    # 传入原始特征向量推理
    curl -X POST http://localhost:5000/predict \
         -H "Content-Type: application/json" \
         -d '{"features": [[0.1, 0.2, ...]], "window_size": 10}'
"""

import argparse
import json
import os
import sys

import numpy as np
import pandas as pd
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
        dropout=0.0,
        num_classes=2,
    ).to(device)

    if os.path.exists(model_path):
        state_dict = torch.load(model_path, map_location=device, weights_only=True)
        model.load_state_dict(state_dict)
        print(f"[serve] model loaded from {model_path}")
    else:
        print(f"[serve] WARNING: model file not found: {model_path}")

    model.eval()

    # 加载 scaler
    scaler_path = os.path.join(os.path.dirname(model_path), "scaler.pkl")
    if os.path.exists(scaler_path):
        import joblib
        scaler = joblib.load(scaler_path)
        print(f"[serve] scaler loaded from {scaler_path}")


def _run_inference(features: np.ndarray, window_size: int) -> list:
    """核心推理逻辑，返回预测结果列表"""
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

    # 构建结果
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

    return predictions


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
    批量预测（传入原始特征向量）

    请求体: {"features": [[f0,f1,...,f149], ...], "window_size": 10}
    响应:   [{"rise_prob":0.72, "fall_prob":0.28, "action":"buy", "confidence":0.72, "expected_pnl":0.15}, ...]
    """
    if model is None:
        return jsonify({"error": "model not loaded"}), 400

    data = request.get_json()
    if not data or "features" not in data:
        return jsonify({"error": "missing 'features' in request body"}), 400

    features = np.array(data["features"], dtype=np.float32)
    window_size = data.get("window_size", 10)

    try:
        predictions = _run_inference(features, window_size)
    except Exception as e:
        return jsonify({"error": str(e)}), 500

    return jsonify(predictions)


@app.route("/predict-csv", methods=["POST"])
def predict_csv():
    """
    基于特征 CSV 文件预测（更易用）

    请求体: {"csv_path": "data/features/features.csv", "window_size": 10}
    响应:
    {
        "total": 417,
        "buy_signals": 85,
        "sell_signals": 42,
        "hold_signals": 290,
        "predictions": [
            {"date":"","symbol":"","time":"09:34:29","rise_prob":0.72, "action":"buy", ...},
            ...
        ]
    }
    """
    if model is None:
        return jsonify({"error": "model not loaded"}), 400

    data = request.get_json()
    if not data or "csv_path" not in data:
        return jsonify({"error": "missing 'csv_path'"}), 400

    csv_path = data["csv_path"]
    window_size = data.get("window_size", 10)

    if not os.path.exists(csv_path):
        return jsonify({"error": f"file not found: {csv_path}"}), 400

    try:
        df = pd.read_csv(csv_path)
        feature_cols = [c for c in df.columns if c.startswith("f")]
        features = df[feature_cols].values.astype(np.float32)
        features = np.nan_to_num(features, nan=0.0, posinf=0.0, neginf=0.0)

        predictions = _run_inference(features, window_size)

        # 附加时间信息
        for i, pred in enumerate(predictions):
            if "date" in df.columns:
                pred["date"] = str(df.iloc[i]["date"])
            if "symbol" in df.columns:
                pred["symbol"] = str(df.iloc[i]["symbol"])
            if "time" in df.columns:
                pred["time"] = str(df.iloc[i]["time"])
            if "label" in df.columns:
                pred["actual_label"] = int(df.iloc[i]["label"])

        # 统计信号分布
        actions = [p["action"] for p in predictions]
        summary = {
            "total": len(predictions),
            "buy_signals": actions.count("buy"),
            "sell_signals": actions.count("sell"),
            "hold_signals": actions.count("hold"),
            "predictions": predictions,
        }
    except Exception as e:
        return jsonify({"error": str(e)}), 500

    return jsonify(summary)


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
    print(f"[serve] 接口列表:")
    print(f"  GET  http://localhost:{args.port}/health")
    print(f"  GET  http://localhost:{args.port}/model-info")
    print(f"  POST http://localhost:{args.port}/predict")
    print(f"  POST http://localhost:{args.port}/predict-csv")
    app.run(host="0.0.0.0", port=args.port, debug=False)


if __name__ == "__main__":
    main()