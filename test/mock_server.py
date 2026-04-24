#!/usr/bin/env python3
"""
Mock推理服务，用于模拟真实场景测试
支持自定义预测结果、延迟模拟、错误注入等功能
"""

import time
import random
from flask import Flask, request, jsonify

app = Flask(__name__)

# 配置参数
CONFIG = {
    "min_delay": 0.01,  # 最小响应延迟(秒)
    "max_delay": 0.05,  # 最大响应延迟(秒)
    "error_rate": 0.0,  # 错误率(0.0-1.0)
    "default_action": "hold",  # 默认动作
    "rise_prob_range": [0.3, 0.7],  # 上涨概率范围
    "confidence_range": [0.5, 0.9],  # 置信度范围
}

# 测试模式配置
TEST_MODES = {
    "normal": {
        "description": "正常模式，随机生成预测结果",
        "action_weights": {"hold": 0.7, "buy": 0.15, "sell": 0.15},
    },
    "bull": {
        "description": "牛市模式，更多买入信号",
        "action_weights": {"hold": 0.5, "buy": 0.4, "sell": 0.1},
    },
    "bear": {
        "description": "熊市模式，更多卖出信号",
        "action_weights": {"hold": 0.5, "buy": 0.1, "sell": 0.4},
    },
    "volatile": {
        "description": "震荡模式，买卖信号交替",
        "action_weights": {"hold": 0.4, "buy": 0.3, "sell": 0.3},
    },
    "error": {
        "description": "错误模式，高错误率",
        "action_weights": {"hold": 1.0},
        "error_rate": 0.3,
    }
}

current_mode = "normal"
signal_counter = 0


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok", "mode": current_mode, "config": CONFIG})


@app.route("/predict", methods=["POST"])
def predict():
    global signal_counter
    
    # 模拟延迟
    delay = random.uniform(CONFIG["min_delay"], CONFIG["max_delay"])
    time.sleep(delay)
    
    # 模拟错误
    if random.random() < CONFIG["error_rate"]:
        return jsonify({"error": "service unavailable"}), 500
    
    # 解析请求
    data = request.get_json()
    features = data.get("features", [])
    window_size = data.get("window_size", 10)
    
    # 获取当前模式配置
    mode_config = TEST_MODES[current_mode]
    action_weights = mode_config["action_weights"]
    
    # 生成预测结果
    actions = list(action_weights.keys())
    weights = list(action_weights.values())
    action = random.choices(actions, weights=weights, k=1)[0]
    
    rise_prob = random.uniform(*CONFIG["rise_prob_range"])
    if action == "buy":
        rise_prob = max(rise_prob, 0.6)
    elif action == "sell":
        rise_prob = min(rise_prob, 0.4)
    
    fall_prob = 1.0 - rise_prob
    confidence = random.uniform(*CONFIG["confidence_range"])
    
    signal_counter += 1
    
    return jsonify([{
        "symbol": "002484",
        "time": "",
        "rise_prob": round(rise_prob, 4),
        "fall_prob": round(fall_prob, 4),
        "action": action,
        "confidence": round(confidence, 4),
        "expected_pnl": round(random.uniform(-0.02, 0.05), 4),
        "signal_id": signal_counter
    }])


@app.route("/config", methods=["POST"])
def update_config():
    """动态更新配置"""
    global CONFIG, current_mode
    data = request.get_json()
    
    if "mode" in data:
        mode = data["mode"]
        if mode in TEST_MODES:
            current_mode = mode
            # 应用模式配置
            mode_config = TEST_MODES[mode]
            if "error_rate" in mode_config:
                CONFIG["error_rate"] = mode_config["error_rate"]
            return jsonify({"status": "ok", "mode": current_mode})
        else:
            return jsonify({"error": f"unknown mode: {mode}"}), 400
    
    if "config" in data:
        CONFIG.update(data["config"])
        return jsonify({"status": "ok", "config": CONFIG})
    
    return jsonify({"error": "invalid request"}), 400


@app.route("/reset", methods=["POST"])
def reset():
    """重置计数器和配置"""
    global signal_counter, current_mode, CONFIG
    signal_counter = 0
    current_mode = "normal"
    CONFIG = {
        "min_delay": 0.01,
        "max_delay": 0.05,
        "error_rate": 0.0,
        "default_action": "hold",
        "rise_prob_range": [0.3, 0.7],
        "confidence_range": [0.5, 0.9],
    }
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="Mock推理服务")
    parser.add_argument("--port", type=int, default=5000, help="监听端口")
    parser.add_argument("--mode", type=str, default="normal", choices=TEST_MODES.keys(), 
                       help="运行模式")
    parser.add_argument("--error-rate", type=float, default=0.0, help="错误率")
    args = parser.parse_args()
    
    current_mode = args.mode
    CONFIG["error_rate"] = args.error_rate
    
    print(f"Mock推理服务启动，端口: {args.port}, 模式: {current_mode}, 错误率: {args.error_rate}")
    print(f"可用模式: {list(TEST_MODES.keys())}")
    app.run(host="0.0.0.0", port=args.port, debug=False)