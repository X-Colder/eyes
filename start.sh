#!/bin/bash
# Eyes Quant System 一键启动脚本

BASE_DIR=$(cd "$(dirname "$0")" && pwd)
LOG_DIR="$BASE_DIR/logs"
mkdir -p $LOG_DIR

echo "🚀 启动 Eyes Quant 量化交易系统..."
echo "=========================================="

# 检查配置文件
if [ ! -f "$BASE_DIR/config.json" ]; then
    echo "❌ 错误: 配置文件 config.json 不存在"
    exit 1
fi

# 端口检查
check_port() {
    if lsof -Pi :$1 -sTCP:LISTEN -t >/dev/null ; then
        echo "⚠️  警告: 端口 $1 已被占用，尝试终止现有进程..."
        lsof -Pi :$1 -sTCP:LISTEN -t | xargs kill -9 2>/dev/null
        sleep 2
    fi
}

# 读取配置中的端口
SERVER_PORT=$(grep -A5 '"server"' $BASE_DIR/config.json | grep '"port"' | cut -d'"' -f4)
ML_PORT=$(grep -A5 '"ml"' $BASE_DIR/config.json | grep '"service_url"' | grep -o ':[0-9]*' | cut -d: -f2)

if [ -z "$SERVER_PORT" ]; then
    SERVER_PORT="8080"
fi
if [ -z "$ML_PORT" ]; then
    ML_PORT="5000"
fi

echo "📋 服务配置:"
echo "   HTTP服务端口: $SERVER_PORT"
echo "   推理服务端口: $ML_PORT"
echo ""

# 检查端口占用
check_port $SERVER_PORT
check_port $ML_PORT

# 启动Python推理服务
echo "1/3 启动Python推理服务..."
cd $BASE_DIR/ml/scripts
nohup python3 serve.py --model-dir ../models --port $ML_PORT > $LOG_DIR/ml_server.log 2>&1 &
ML_PID=$!
echo "   推理服务PID: $ML_PID"
echo "   日志文件: $LOG_DIR/ml_server.log"

# 等待推理服务启动
echo "   等待推理服务就绪..."
for i in {1..30}; do
    if curl -s http://localhost:$ML_PORT/health >/dev/null 2>&1; then
        echo "   ✅ 推理服务启动成功"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "   ❌ 推理服务启动超时，请检查日志 $LOG_DIR/ml_server.log"
        kill $ML_PID 2>/dev/null
        exit 1
    fi
    sleep 1
done

# 启动Go HTTP服务
echo ""
echo "2/3 启动Go HTTP服务..."
cd $BASE_DIR
if [ -f "$BASE_DIR/bin/eyes-server" ]; then
    nohup $BASE_DIR/bin/eyes-server --config config.json > $LOG_DIR/server.log 2>&1 &
else
    nohup go run cmd/server/main.go --config config.json > $LOG_DIR/server.log 2>&1 &
fi
SERVER_PID=$!
echo "   HTTP服务PID: $SERVER_PID"
echo "   日志文件: $LOG_DIR/server.log"

# 等待HTTP服务启动
echo "   等待HTTP服务就绪..."
for i in {1..30}; do
    if curl -s http://localhost:$SERVER_PORT/api/health >/dev/null 2>&1; then
        echo "   ✅ HTTP服务启动成功"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "   ❌ HTTP服务启动超时，请检查日志 $LOG_DIR/server.log"
        kill $SERVER_PID $ML_PID 2>/dev/null
        exit 1
    fi
    sleep 1
done

# 保存PID到文件
echo $SERVER_PID > $LOG_DIR/server.pid
echo $ML_PID > $LOG_DIR/ml_server.pid

echo ""
echo "3/3 启动闭环处理服务..."
cd $BASE_DIR
nohup go run cmd/pipeline/main.go > $LOG_DIR/pipeline.log 2>&1 &
PIPELINE_PID=$!
echo $PIPELINE_PID > $LOG_DIR/pipeline.pid
echo "   闭环处理服务PID: $PIPELINE_PID"
echo "   日志文件: $LOG_DIR/pipeline.log"

echo ""
echo "=========================================="
echo "✅ 所有服务启动完成!"
echo ""
echo "📌 服务地址:"
echo "   HTTP API: http://localhost:$SERVER_PORT"
echo "   推理服务: http://localhost:$ML_PORT"
echo ""
echo "📝 常用命令:"
echo "   停止服务: ./stop.sh"
echo "   查看状态: ./status.sh"
echo "   查看日志: tail -f logs/server.log"
echo ""
echo "🎯 系统已准备就绪，可以接收tick数据进行自动交易!"