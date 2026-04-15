# Eyes 量化交易系统 - 部署文档

## 一、系统要求

### 1.1 开发环境（本地打包机）

- macOS / Linux
- Go 1.24+
- Python 3.9+
- 网络连接（下载 PyTorch whl 和依赖包）

### 1.2 目标服务器（GPU 训练机）

- Ubuntu 22.04
- NVIDIA GPU（推荐 A800/A100）
- NVIDIA Driver 535+
- CUDA 12.2（也支持 12.1）
- Python 3.9+ 与 pip
- 网络连接（pip 在线安装依赖）

---

## 二、在线部署（有网络）

### 2.1 安装 Python 依赖

```bash
pip3 install -r ml/requirements.txt
```

依赖包括：torch, numpy, pandas, scikit-learn, flask, joblib, matplotlib, pyyaml

### 2.2 编译 Go 后端

```bash
# 本地编译
make build

# 交叉编译 Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/eyes-server ./cmd/server
```

### 2.3 启动服务

```bash
# 1. 启动 Go 后端
./bin/eyes-server -config config.json

# 2. 启动 Python 推理服务
python3 ml/scripts/serve.py --model data/models/best_model.pt --port 5000
```

---

## 三、服务器部署

### 3.1 本地打包

在开发机上执行：

```bash
# 默认 Linux amd64
make pack

# ARM64 架构
GPU_ARCH=arm64 bash scripts/pack.sh
```

产出文件：`dist/eyes-deploy.tar.gz`

### 3.2 传输到服务器

```bash
scp dist/eyes-deploy.tar.gz user@gpu-server:/opt/eyes/
```

### 3.3 服务器端安装

```bash
cd /opt/eyes
tar xzf eyes-deploy.tar.gz
cd eyes-deploy

# 在线安装 Python 依赖
bash install.sh
```

`install.sh` 会自动：
1. 检测 GPU 和 CUDA
2. 通过 pip 在线安装 PyTorch 和其他依赖
3. 验证 torch.cuda.is_available()

### 3.4 训练模型

```bash
bash train.sh
```

`train.sh` 会自动：
1. 检测可用 GPU 数量
2. 单卡用 `python3`，多卡用 `torchrun --nproc_per_node=N`
3. 训练完成后模型保存在 `data/models/`

### 3.5 启动推理服务

```bash
python3 ml/scripts/serve.py \
    --model data/models/best_model.pt \
    --port 5000
```

### 3.6 启动 Go 后端

```bash
./bin/eyes-server -config config.json
```

---

## 四、打包内容说明

```
eyes-deploy/
├── bin/eyes-server          # Go 后端二进制（Linux amd64）
├── ml/
│   ├── models/              # Transformer 模型定义
│   ├── scripts/             # 训练和推理脚本
│   └── requirements.txt     # Python 依赖清单
├── data/
│   └── features/            # 预处理特征文件
├── config.json              # 全局配置
├── *.csv                    # tick 数据文件
├── install.sh               # 在线安装脚本
└── train.sh                 # 训练启动脚本
```

---

## 五、配置说明

`config.json` 核心配置项：

```json
{
    "server": {"port": "8080"},
    "data": {
        "tick_dir": ".",
        "output_dir": "data/features"
    },
    "feature": {
        "bar_interval": 30,
        "window_size": 10,
        "future_steps": 3,
        "price_thresh": 0.02
    },
    "ml": {
        "model_dir": "data/models",
        "script_dir": "ml/scripts",
        "python_path": "python3",
        "service_url": "http://localhost:5000"
    },
    "backtest": {
        "initial_cash": 100000,
        "commission": 0.0003,
        "slippage": 0.001,
        "max_position": 10000
    },
    "pipeline": {
        "symbol": "002484",
        "train_ratio": 0.7,
        "retrain_after": 3,
        "feature_dim": 24
    }
}
```

| 配置项 | 说明 |
|--------|------|
| bar_interval | tick 聚合间隔秒数 |
| window_size | 滑动窗口大小（bar 数） |
| future_steps | 预测未来 N 根 bar |
| price_thresh | 涨跌标签阈值（%） |
| train_ratio | 训练天数占比（pipeline 模式） |
| retrain_after | 每推理 N 天后再训练（0=不再训练） |

---

## 六、多 GPU 训练

系统支持 DDP 分布式训练，自动检测 GPU 数量：

```bash
# 单卡
python3 ml/scripts/train.py --data features.csv --model-dir data/models

# 多卡（8 卡 A800）
torchrun --nproc_per_node=8 ml/scripts/train.py --data features.csv --model-dir data/models

# 或直接用 train.sh（自动检测）
bash train.sh
```

DDP 特性：
- NCCL 后端通信
- DistributedSampler 数据分片
- 学习率线性缩放（lr × world_size）
- all_reduce 同步 loss/accuracy
- broadcast 同步 early stopping
- 仅 rank 0 保存模型

---

## 七、健康检查

```bash
# Go 后端
curl http://localhost:8080/api/health
# => {"status":"ok"}

# Python 推理
curl http://localhost:5000/health
# => {"status":"ok","model_loaded":true}
```