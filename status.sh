#!/bin/bash
# 查看服务状态脚本

BASE_DIR=$(cd "$(dirname "$0")" && pwd)
LOG_DIR="$BASE_DIR/logs"

echo "📊 Eyes Quant 系统状态"
echo "======================"

# 检查进程状态
check_service() {
    local name=$1
    local pid_file="$LOG_DIR/$2"
    local port=$3
    
    echo -n "[$name] "
    
    # 检查PID文件
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            echo -n "✅ 运行中 (PID: $pid)"
            
            # 检查端口监听
            if [ ! -z "$port" ]; then
                if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null ; then
                    echo -n ", 端口 $port 正常"
                else
                    echo -n ", ⚠️  端口 $port 未监听"
                fi
            fi
            echo ""
        else
            echo "❌ 已停止 (PID文件存在但进程不存在)"
        fi
    else
        # 尝试查找进程
        if pgrep -f "$4" >/dev/null 2>&1; then
            local pid=$(pgrep -f "$4" | head -1)
            echo "⚠️  运行中 (PID: $pid, 无PID文件)"
        else
            echo "❌ 未运行"
        fi
    fi
}

# 读取配置端口
SERVER_PORT=$(grep -A5 '"server"' $BASE_DIR/config.json | grep '"port"' | cut -d'"' -f4 2>/dev/null)
ML_PORT=$(grep -A5 '"ml"' $BASE_DIR/config.json | grep '"service_url"' | grep -o ':[0-9]*' | cut -d: -f2 2>/dev/null)

if [ -z "$SERVER_PORT" ]; then
    SERVER_PORT="8080"
fi
if [ -z "$ML_PORT" ]; then
    ML_PORT="5000"
fi

echo ""
echo "📌 服务状态:"
check_service "HTTP服务" "server.pid" "$SERVER_PORT" "eyes-server\|go run cmd/server"
check_service "推理服务" "ml_server.pid" "$ML_PORT" "python3 serve.py"
check_service "闭环处理" "pipeline.pid" "" "go run cmd/pipeline"

# 检查系统资源
echo ""
echo "💻 系统资源:"
if [ -f "$LOG_DIR/server.pid" ]; then
    SERVER_PID=$(cat "$LOG_DIR/server.pid")
    if kill -0 "$SERVER_PID" 2>/dev/null; then
        SERVER_MEM=$(ps -o rss= -p $SERVER_PID | awk '{print int($1/1024)"MB"}')
        echo "   HTTP服务内存: $SERVER_MEM"
    fi
fi

if [ -f "$LOG_DIR/ml_server.pid" ]; then
    ML_PID=$(cat "$LOG_DIR/ml_server.pid")
    if kill -0 "$ML_PID" 2>/dev/null; then
        ML_MEM=$(ps -o rss= -p $ML_PID | awk '{print int($1/1024)"MB"}')
        echo "   推理服务内存: $ML_MEM"
    fi
fi

# 磁盘空间
echo "   磁盘使用: $(df -h $BASE_DIR | tail -1 | awk '{print $5 " used, " $4 " free"}')"

# 最新日志
echo ""
echo "📝 最新日志 (最近5行):"
if [ -f "$LOG_DIR/server.log" ]; then
    echo "--- HTTP服务日志 ---"
    tail -5 "$LOG_DIR/server.log"
fi
if [ -f "$LOG_DIR/pipeline.log" ]; then
    echo "--- 闭环服务日志 ---"
    tail -5 "$LOG_DIR/pipeline.log"
fi

echo ""
echo "📍 服务地址:"
echo "   HTTP API: http://localhost:$SERVER_PORT"
echo "   推理服务: http://localhost:$ML_PORT"
echo "   健康检查: curl http://localhost:$SERVER_PORT/api/health"