#!/bin/bash
# 停止所有服务脚本

BASE_DIR=$(cd "$(dirname "$0")" && pwd)
LOG_DIR="$BASE_DIR/logs"

echo "🛑 停止 Eyes Quant 系统服务..."

# 读取PID文件并停止进程
stop_service() {
    local name=$1
    local pid_file="$LOG_DIR/$2"
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            echo "   停止 $name 服务 (PID: $pid)..."
            kill "$pid" 2>/dev/null
            sleep 1
            if kill -0 "$pid" 2>/dev/null; then
                echo "   强制终止 $name 服务..."
                kill -9 "$pid" 2>/dev/null
            fi
            echo "   ✅ $name 服务已停止"
        else
            echo "   $name 服务未运行"
        fi
        rm -f "$pid_file"
    else
        echo "   $name PID文件不存在，跳过"
    fi
}

# 停止所有服务
stop_service "闭环处理" "pipeline.pid"
stop_service "HTTP服务" "server.pid"
stop_service "推理服务" "ml_server.pid"

# 清理残留进程
echo ""
echo "清理残留进程..."
pkill -f "eyes-server" 2>/dev/null
pkill -f "go run cmd/server" 2>/dev/null
pkill -f "python3 serve.py" 2>/dev/null
pkill -f "go run cmd/pipeline" 2>/dev/null

echo ""
echo "✅ 所有服务已停止"