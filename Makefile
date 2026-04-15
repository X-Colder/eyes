.PHONY: build run clean setup train serve test all

# Go 后端
build:
	go build -o bin/eyes-server ./cmd/server

run: build
	./bin/eyes-server -config config.json

# Python ML 环境
setup:
	pip3 install -r ml/requirements.txt

# 完整流程：加载数据 -> 导出特征 -> 训练 -> 启动服务
all: build
	@echo "=== Step 1: 启动 Go 服务并加载数据 ==="
	./bin/eyes-server -config config.json &
	@sleep 2
	@echo "=== Step 2: 加载 tick 数据并提取特征 ==="
	curl -s http://localhost:8080/api/load | python3 -m json.tool
	@echo "=== Step 3: 导出特征文件 ==="
	curl -s http://localhost:8080/api/export | python3 -m json.tool
	@echo "=== Step 4: 训练 Transformer 模型 ==="
	python3 ml/scripts/train.py --data data/features/features.csv --model-dir data/models --window-size 10
	@echo "=== Step 5: 回测 ==="
	curl -s http://localhost:8080/api/backtest | python3 -m json.tool
	@echo "=== Done ==="

# 单独训练
train:
	python3 ml/scripts/train.py \
		--data data/features/features.csv \
		--model-dir data/models \
		--window-size 10 \
		--epochs 100 \
		--batch-size 32

# 启动推理服务
serve:
	python3 ml/scripts/serve.py \
		--model data/models/best_model.pt \
		--port 5000

# 测试 Go 代码
test:
	go test ./...

# 离线打包（在有网的本地机器上执行，产出 dist/eyes-deploy.tar.gz）
pack:
	bash scripts/pack.sh

# 指定 GPU 服务器为 arm64 架构时
pack-arm:
	GPU_ARCH=arm64 bash scripts/pack.sh

# 清理
clean:
	rm -rf bin/
	rm -rf data/features/
	rm -rf data/models/
	rm -rf dist/