#!/bin/bash

# 启动监控系统脚本

echo "========================================"
echo "  启动量化交易监控系统"
echo "========================================"

# 编译监控服务
echo "编译监控服务..."
go build -o bin/monitor-server ./cmd/monitor

if [ $? -ne 0 ]; then
    echo "编译失败!"
    exit 1
fi

echo "编译成功!"
echo ""
echo "启动监控服务..."
echo "访问地址: http://localhost:8082"
echo ""

# 启动服务
./bin/monitor-server