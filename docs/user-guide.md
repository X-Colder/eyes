# Eyes 量化交易系统 - 使用手册

## 一、系统架构

```
tick CSV → Go 后端 → 特征工程 → Python Transformer → 交易信号 → 回测/闭环
           (8080)                    (5000)
```

Eyes 是一个 Go + Python 双语言量化交易系统：
- **Go 后端**: 数据加载、特征工程、趋势分析、回测引擎、信号引擎、闭环编排、REST API
- **Python ML**: Transformer 模型训练（DDP 多卡）、推理服务（Flask）

---

## 二、快速开始

### 2.1 环境准备

```bash
# 安装 Python 依赖
pip3 install -r ml/requirements.txt

# 编译 Go 后端
make build
```

### 2.2 完整流程（一键执行）

```bash
make all
```

这会依次执行：启动 Go 服务 → 加载数据 → 导出特征 → 训练模型 → 回测

### 2.3 手动分步执行

```bash
# 1. 启动 Go 后端
make run

# 2. 加载 tick 数据
curl http://localhost:8080/api/load

# 3. 导出特征 CSV
curl http://localhost:8080/api/export

# 4. 训练模型
make train

# 5. 启动推理服务
make serve

# 6. 回测
curl http://localhost:8080/api/backtest
```

---

## 三、API 接口说明

### 3.1 基础接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/health | 健康检查 |
| GET | /api/load?file=002484.csv | 加载单日 tick 数据 |
| GET | /api/load-all?dir=.&symbol=002484 | 批量加载多日数据 |
| GET | /api/stats | 日度统计信息 |
| GET | /api/bars | 聚合后的 K 线数据 |
| GET | /api/features | 特征数据摘要 |
| GET | /api/export | 导出特征 CSV 和 JSON |
| POST | /api/train | 启动模型训练 |
| GET | /api/predict | 调用推理服务预测 |
| GET | /api/backtest | 执行回测 |
| POST | /api/pipeline/run | 启动闭环流水线 |
| GET | /api/pipeline/status | 查询闭环流水线状态 |

### 3.2 加载数据

```bash
# 加载单日数据
curl "http://localhost:8080/api/load?file=002484.csv"
# => {"ticks":3421, "bars":430, "features":417, "stats":{...}}

# 批量加载多日数据
curl "http://localhost:8080/api/load-all?dir=.&symbol=002484"
# => {"days":5, "dates":["2018-05-18","2018-05-21",...], "total_bars":2150, ...}
```

CSV 文件命名规范：
- `002484_2018-05-18.csv` — 自动识别标的和日期
- `002484_20180518.csv` — 也支持无分隔符日期
- `002484.csv` — 需要指定 default_date 参数

### 3.3 导出特征

```bash
curl "http://localhost:8080/api/export"
# => {"features_csv":"data/features/features.csv", "data_json":"data/features/data.json"}
```

特征 CSV 格式：`date, symbol, time, f0, f1, ..., f149, label, price_chg, phase`

### 3.4 回测

```bash
curl "http://localhost:8080/api/backtest"
# =>
# {
#   "total_return": 2.35,
#   "win_rate": 65.0,
#   "max_drawdown": 1.2,
#   "sharpe_ratio": 1.85,
#   "trade_count": 12,
#   "trades": [...]
# }
```

---

## 四、推理服务

### 4.1 启动

```bash
python3 ml/scripts/serve.py \
    --model data/models/best_model.pt \
    --port 5000 \
    --feature-dim 24
```

### 4.2 推理接口

```bash
# 方式一：传入特征向量
curl -X POST http://localhost:5000/predict \
     -H "Content-Type: application/json" \
     -d '{"features": [[0.1, 0.2, ...共150维...]], "window_size": 10}'

# 方式二：传入 CSV 文件路径（推荐）
curl -X POST http://localhost:5000/predict-csv \
     -H "Content-Type: application/json" \
     -d '{"csv_path": "data/features/features.csv", "window_size": 10}'
# =>
# {
#   "total": 417,
#   "buy_signals": 37,
#   "sell_signals": 118,
#   "hold_signals": 262,
#   "predictions": [{
#     "date": "2018-05-18",
#     "time": "09:34:29",
#     "action": "buy",
#     "rise_prob": 0.72,
#     "confidence": 0.72,
#     ...
#   }, ...]
# }
```

---

## 五、闭环流水线（Pipeline）

闭环模式是系统的核心功能，实现：**训练 → 推理 → 交易 → 回测 → 再训练** 的自动循环。

### 5.1 启动闭环

**前提**: 确保推理服务已启动（端口 5000）

```bash
# 启动 Go 后端
./bin/eyes-server -config config.json

# 启动推理服务
python3 ml/scripts/serve.py --model data/models/best_model.pt --port 5000

# 触发闭环
curl -X POST http://localhost:8080/api/pipeline/run
# => {"status":"pipeline_started","message":"use /api/pipeline/status to check progress"}
```

### 5.2 查看进度

```bash
curl http://localhost:8080/api/pipeline/status
# =>
# {
#   "symbol": "002484",
#   "phase": "infer",          // idle / train / infer / retrain / done
#   "current_day": "2018-05-22",
#   "model_version": 1,
#   "cash": 99850.5,
#   "cumulative_pnl": -149.5,
#   "daily_results": [
#     {
#       "date": "2018-05-21",
#       "signal_count": 320,
#       "buy_signals": 45,
#       "sell_signals": 80,
#       "trade_count": 3,
#       "day_pnl": -149.5,
#       "win_rate": 33.3
#     }
#   ]
# }
```

### 5.3 闭环流程

1. 加载 tick_dir 下所有 CSV 文件
2. 按 `train_ratio`（默认 0.7）划分训练天和推理天
3. 用训练天数据导出特征 → 训练 Transformer 模型
4. 等待推理服务就绪
5. 逐天推理：
   - 将 tick 聚合为 30s bar
   - 滑动窗口提取特征
   - 调用推理服务获取预测
   - 生成交易信号（含胜率/赔率/Kelly 仓位/目标价/止损价）
   - 执行交易（开仓/平仓）
   - 日终强制平仓
6. 每 `retrain_after` 天（默认 3 天）追加已推理数据再训练
7. 输出完整交易记录和收益统计

### 5.4 交易信号字段

每个信号包含以下信息：

| 字段 | 说明 |
|------|------|
| action | buy / sell / hold |
| confidence | 预测置信度 |
| current_price | 当前价格 |
| target_price | 目标价格 |
| stop_loss_price | 止损价格 |
| volume | 建议交易量 |
| win_rate | 预估胜率 |
| odds_ratio | 赔率（盈亏比） |
| profit_rate | 预期利润率 % |
| kelly_fraction | Kelly 最优仓位比例 |
| hold_bars | 建议持有 bar 数 |
| hold_seconds | 建议持有秒数 |
| phase | 当前趋势阶段 |

---

## 六、数据格式

### 6.1 tick CSV 格式

```csv
TranID,Time,Price,Volume,SaleOrderVolume,BuyOrderVolume,Type,SaleOrderID,SaleOrderPrice,BuyOrderID,BuyOrderPrice
172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84
```

11 列：成交编号、时间、价格、成交量、卖方委托量、买方委托量、类型(B/S)、卖方委托号、卖方委托价、买方委托号、买方委托价

### 6.2 多日数据目录

将多日 CSV 文件放在同一目录下：

```
data/
├── 002484_2018-05-18.csv
├── 002484_2018-05-21.csv
├── 002484_2018-05-22.csv
└── ...
```

---

## 七、模型训练参数

```bash
python3 ml/scripts/train.py \
    --data data/features/features.csv \
    --model-dir data/models \
    --window-size 10 \
    --epochs 100 \
    --batch-size 32 \
    --lr 0.001 \
    --d-model 64 \
    --nhead 4 \
    --num-layers 3
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| --data | 必填 | 特征 CSV 文件路径 |
| --data-dir | | 多文件目录（与 --data 二选一） |
| --model-dir | data/models | 模型保存目录 |
| --window-size | 10 | 滑动窗口大小 |
| --epochs | 100 | 训练轮数 |
| --batch-size | 32 | 批次大小 |
| --lr | 0.001 | 学习率（多卡自动缩放） |
| --patience | 15 | Early stopping 耐心值 |

---

## 八、Makefile 命令

```bash
make build    # 编译 Go 后端
make run      # 编译并启动服务
make setup    # 安装 Python 依赖
make all      # 完整流程（加载→导出→训练→回测）
make train    # 单独训练模型
make serve    # 启动推理服务
make test     # 运行 Go 测试
make pack     # 离线打包
make clean    # 清理构建产物
```