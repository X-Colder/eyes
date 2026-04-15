#!/bin/bash
# ============================================================
# Eyes 量化系统 - 部署打包脚本
#
# 打包 Go 二进制 + Python 代码 + 数据 + 部署脚本，
# 传到 Ubuntu 22.04 GPU 服务器后在线安装 pip 依赖即可训练。
#
# 用法：
#   bash scripts/pack.sh
#
# 服务器上：
#   tar xzf eyes-deploy.tar.gz
#   cd eyes-deploy
#   bash install.sh          # 在线安装 Python 依赖
#   bash train.sh            # 一键训练
# ============================================================

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${PROJECT_ROOT}/dist"
PACK_DIR="${DIST_DIR}/eyes-deploy"
GPU_OS="${GPU_OS:-linux}"
GPU_ARCH="${GPU_ARCH:-amd64}"

echo "=== Eyes 部署打包 ==="
echo "  目标平台: ${GPU_OS}/${GPU_ARCH}"
echo ""

# 清理
rm -rf "${PACK_DIR}"
mkdir -p "${PACK_DIR}"

# -----------------------------------------------------------
# 1. Go 交叉编译（静态链接，服务器无需装 Go）
# -----------------------------------------------------------
echo ">>> Step 1/4: Go 交叉编译"
cd "${PROJECT_ROOT}"
CGO_ENABLED=0 GOOS=${GPU_OS} GOARCH=${GPU_ARCH} \
  go build -ldflags="-s -w" -o "${PACK_DIR}/bin/eyes-server" ./cmd/server
echo "  -> bin/eyes-server OK"

# -----------------------------------------------------------
# 2. 拷贝项目文件
# -----------------------------------------------------------
echo ""
echo ">>> Step 2/4: 拷贝项目文件"

cp -r "${PROJECT_ROOT}/ml" "${PACK_DIR}/ml"
cp "${PROJECT_ROOT}/config.json" "${PACK_DIR}/"

mkdir -p "${PACK_DIR}/data/features" "${PACK_DIR}/data/models"
cp "${PROJECT_ROOT}"/*.csv "${PACK_DIR}/data/" 2>/dev/null || true

if [ -d "${PROJECT_ROOT}/data/features" ]; then
  cp -r "${PROJECT_ROOT}/data/features" "${PACK_DIR}/data/features"
fi

echo "  -> OK"

# -----------------------------------------------------------
# 3. 生成安装脚本 + 一键训练脚本
# -----------------------------------------------------------
echo ""
echo ">>> Step 3/4: 生成部署脚本"

# ---- install.sh ----
cat > "${PACK_DIR}/install.sh" << 'EOF'
#!/bin/bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Eyes 环境安装 ==="

# 检查 Python
if ! command -v python3 &>/dev/null; then
  echo "ERROR: 未找到 python3"; exit 1
fi
echo "  Python: $(python3 --version)"

# 检查 CUDA
if command -v nvidia-smi &>/dev/null; then
  echo "  GPU 数量: $(nvidia-smi --query-gpu=name --format=csv,noheader | wc -l)"
  nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader
  echo "  驱动: $(nvidia-smi --query-gpu=driver_version --format=csv,noheader | head -1)"
else
  echo "  WARNING: 未检测到 nvidia-smi，将使用 CPU 训练"
fi

# 安装 Python 包（在线）
echo ""
echo ">>> 安装 Python 依赖..."
pip3 install -r "${DIR}/ml/requirements.txt"

# 验证 torch + CUDA
python3 -c "
import torch
print(f'  torch {torch.__version__}')
print(f'  CUDA available: {torch.cuda.is_available()}')
if torch.cuda.is_available():
    print(f'  GPU: {torch.cuda.get_device_name(0)}')
"

chmod +x "${DIR}/bin/eyes-server"
echo ""
echo "=== 安装完成 ==="
echo "  接下来运行: bash train.sh"
EOF
chmod +x "${PACK_DIR}/install.sh"

# ---- train.sh ----
cat > "${PACK_DIR}/train.sh" << 'EOF'
#!/bin/bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Eyes 一键训练 ==="

# 1. 启动 Go 服务，加载数据并导出特征
echo ">>> 启动 Go 服务..."
cd "${DIR}"

# 修正 config 中的数据路径
python3 -c "
import json
with open('config.json') as f: c = json.load(f)
c['data']['tick_dir'] = 'data'
c['data']['output_dir'] = 'data/features'
c['ml']['model_dir'] = 'data/models'
with open('config.json','w') as f: json.dump(c, f, indent=2)
print('  config.json 路径已修正')
"

./bin/eyes-server &
SERVER_PID=$!
sleep 2

# 判断是否有多个 CSV（多日数据）
CSV_COUNT=$(ls data/*.csv 2>/dev/null | wc -l)
echo "  发现 ${CSV_COUNT} 个 CSV 文件"

if [ "${CSV_COUNT}" -gt 1 ]; then
  echo ">>> 加载多日数据..."
  curl -s "http://localhost:8080/api/load-all?dir=data&symbol=002484" | python3 -m json.tool
else
  echo ">>> 加载单日数据..."
  curl -s "http://localhost:8080/api/load" | python3 -m json.tool
fi

echo ">>> 导出特征..."
curl -s "http://localhost:8080/api/export" | python3 -m json.tool

# 关闭 Go 服务
kill ${SERVER_PID} 2>/dev/null || true
echo "  Go 服务已关闭"

# 2. 训练模型
echo ""

# 检测 GPU 数量，自动选择单卡或多卡
NUM_GPUS=$(python3 -c "import torch; print(torch.cuda.device_count())" 2>/dev/null || echo "0")
# 可通过环境变量覆盖使用的卡数: NGPUS=4 bash train.sh
NGPUS="${NGPUS:-${NUM_GPUS}}"

TRAIN_ARGS="--data data/features/features.csv \
  --model-dir data/models \
  --window-size 10 \
  --epochs 100 \
  --batch-size 32 \
  --d-model 64 \
  --nhead 4 \
  --num-layers 3"

if [ "${NGPUS}" -gt 1 ]; then
  echo ">>> 多卡训练 (${NGPUS} GPUs, DDP)..."
  torchrun --nproc_per_node=${NGPUS} ml/scripts/train.py ${TRAIN_ARGS}
else
  echo ">>> 单卡训练..."
  python3 ml/scripts/train.py ${TRAIN_ARGS}
fi

echo ""
echo "=== 训练完成 ==="
echo "  模型保存在: data/models/best_model.pt"
echo "  报告: data/models/training_report.json"
echo ""
echo "  启动推理服务: python3 ml/scripts/serve.py --model data/models/best_model.pt"
echo ""
echo "  手动指定卡数: NGPUS=4 bash train.sh"
EOF
chmod +x "${PACK_DIR}/train.sh"

echo "  -> install.sh, train.sh 已生成"

# -----------------------------------------------------------
# 4. 打包
# -----------------------------------------------------------
echo ""
echo ">>> Step 4/4: 打包"
cd "${DIST_DIR}"
tar czf eyes-deploy.tar.gz eyes-deploy/
FINAL_SIZE=$(du -sh eyes-deploy.tar.gz | cut -f1)
echo ""
echo "=== 打包完成 ==="
echo "  文件: dist/eyes-deploy.tar.gz (${FINAL_SIZE})"
echo ""
echo "  部署步骤："
echo "    1. scp dist/eyes-deploy.tar.gz user@gpu-server:~/"
echo "    2. ssh gpu-server"
echo "    3. tar xzf eyes-deploy.tar.gz && cd eyes-deploy"
echo "    4. bash install.sh    # 在线安装 Python 依赖"
echo "    5. bash train.sh      # 一键训练"