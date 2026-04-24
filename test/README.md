# 实盘交易模拟测试指南

## 测试架构
```
历史tick数据 → SignalEngine → Mock推理服务
       ↓            ↓
    交易信号       预测结果
       ↓            ↓
    交易执行 → 结果统计 → 报告生成
```

## 环境准备

### 1. 安装Mock服务依赖
```bash
cd test
pip install flask
```

### 2. 启动Mock推理服务
```bash
# 基础模式启动
python mock_server.py --port 5000 --mode normal

# 牛市模式
python mock_server.py --mode bull

# 错误注入模式（30%错误率）
python mock_server.py --mode error --error-rate 0.3
```

### 3. 可用运行模式
| 模式 | 描述 | 买卖信号比例 |
|------|------|-------------|
| `normal` | 正常市场 | hold:70%, buy:15%, sell:15% |
| `bull` | 牛市 | hold:50%, buy:40%, sell:10% |
| `bear` | 熊市 | hold:50%, buy:10%, sell:40% |
| `volatile` | 震荡市 | hold:40%, buy:30%, sell:30% |
| `error` | 错误模式 | 30%错误率 |

## 测试执行流程

### 1. 运行所有模拟测试
```bash
# 确保Mock服务已启动
cd test
go test -v -run="Test.*"
```

### 2. 运行单个测试
```bash
# 基础功能测试
go test -v -run="TestSimulation"

# 牛市场景测试
go test -v -run="TestBullMarket"

# 熊市场景测试
go test -v -run="TestBearMarket"

# 错误注入测试
go test -v -run="TestErrorInjection"
```

### 3. 运行自定义参数测试
创建自定义测试用例：
```go
func TestCustomScenario(t *testing.T) {
    config := SimulationConfig{
        Name:        "自定义场景",
        Description: "高滑点高手续费场景测试",
        Mode:        "volatile",
        ErrorRate:   0.1,
        InitialCash: 100000,
        Commission:  0.001,  // 千1手续费
        Slippage:    0.003,  // 千3滑点
        MaxPosition: 5000,   // 最大持仓5000股
        BarInterval: 30,
        WindowSize:  10,
        FutureSteps: 3,
        PriceThresh: 0.02,
    }
    
    result, err := RunSimulation(config, "../002484.csv")
    // 自定义断言...
}
```

## 测试结果验证

### 1. 结果文件
测试结果会保存在 `test/results/` 目录下：
- `simulation_result.json` - 基础测试结果
- `bull_market_result.json` - 牛市测试结果
- `bear_market_result.json` - 熊市测试结果
- `error_injection_result.json` - 错误注入测试结果

### 2. 核心验证指标

#### 必须通过的基础检查
```go
// 1. 资金安全检查
assert.GreaterOrEqual(t, result.TotalReturn, -50.0, "最大亏损不能超过50%")
assert.Equal(t, pos.Side, "flat", "测试结束必须空仓")

// 2. 逻辑一致性检查
for _, trade := range result.Trades {
    assert.NotEmpty(t, trade.EntryTime, "开仓时间不能为空")
    assert.NotEmpty(t, trade.ExitTime, "平仓时间不能为空")
    assert.Greater(t, trade.Volume, int64(0), "成交量必须大于0")
    assert.Greater(t, trade.EntryPrice, 0.0, "开仓价格必须大于0")
    assert.Greater(t, trade.ExitPrice, 0.0, "平仓价格必须大于0")
}

// 3. 风险控制检查
max_position := int64(10000)
for _, signal := range signals {
    assert.LessOrEqual(t, signal.Volume, max_position, "不能超过最大持仓限制")
}
```

#### 性能指标验证
| 指标 | 合格标准 | 优秀标准 |
|------|----------|----------|
| 单tick处理延迟 | < 10ms | < 5ms |
| 信号生成延迟 | < 50ms | < 20ms |
| 交易成功率 | > 99% | 100% |
| 错误场景下错误交易 | 0 | 0 |

#### 不同场景的预期表现
| 场景 | 预期交易次数 | 预期收益率 | 预期胜率 |
|------|-------------|-----------|---------|
| 正常市场 | 5-20次 | ±20% | 40-60% |
| 牛市 | 10-30次 | +5% ~ +30% | 50-70% |
| 熊市 | 5-15次 | -10% ~ +10% | 30-50% |
| 震荡市 | 15-30次 | -5% ~ +15% | 40-60% |
| 错误注入 | < 正常市场的70% | -15% ~ +10% | 无要求 |

### 3. 结果分析工具

#### 生成Markdown测试报告
```bash
# 生成单个测试报告
python3 generate_report.py results/simulation_result.json

# 指定输出路径
python3 generate_report.py results/simulation_result.json reports/simulation_report.md

# 批量生成所有测试报告
for f in results/*.json; do python3 generate_report.py $f; done
```

#### 快速查看核心指标
```bash
# 查看收益率和交易次数
jq '.TotalReturn, .TradeCount, .WinRate' test/results/simulation_result.json
```

#### 交易明细分析
```bash
# 查看盈利交易
jq '.Trades[] | select(.PnL > 0)' test/results/simulation_result.json

# 查看亏损交易
jq '.Trades[] | select(.PnL < 0)' test/results/simulation_result.json

# 计算平均每笔盈利/亏损
jq '[.Trades[].PnL] | add / length' test/results/simulation_result.json
```

## 进阶测试场景

### 1. 极端行情测试
```go
// 构造极端涨跌行情测试
func TestExtremeMarket(t *testing.T) {
    // 生成连续涨停/跌停tick数据
    ticks := generateExtremeTicks(1000, 0.1) // 10%涨跌停
    
    // 测试信号引擎在极端行情下的表现
    // ...
}
```

### 2. 性能压力测试
```bash
# 批量运行100次测试，验证稳定性
for i in {1..100}; do
    go test -v -run="TestSimulation" >> test_log.txt
done
```

### 3. 多日连续测试
```go
// 加载多日数据，测试连续运行稳定性
func TestMultiDaySimulation(t *testing.T) {
    dayTicks := loader.LoadMultiDayDir("../data/ticks", "002484", "")
    
    se := engine.NewSignalEngine(...)
    for _, dt := range dayTicks {
        signals, trades := se.ProcessDayTicks(dt.Date, dt.Symbol, dt.Ticks)
        // 验证每日状态一致性
        // ...
    }
}
```

## 测试报告模板

```
# 模拟测试报告

## 测试基本信息
- 测试时间: 2024-01-01
- 测试版本: commit xxx
- 测试场景: 牛市环境
- 初始资金: 100000元

## 测试结果
- 总收益率: 18.5%
- 交易次数: 24次
- 胜率: 62.5%
- 最大回撤: 8.2%
- 夏普比率: 1.8
- 测试时长: 2.3s

## 交易明细
| 交易ID | 开仓时间 | 平仓时间 | 开仓价 | 平仓价 | 盈亏 | 收益率 |
|--------|----------|----------|--------|--------|------|--------|
| 1 | 09:30:30 | 09:35:00 | 7.85 | 7.95 | +1000 | +1.27% |
| ... | ... | ... | ... | ... | ... | ... |

## 问题与优化建议
1. 发现问题：在连续下跌行情中止损不够及时
2. 优化建议：调整止损参数，从1倍波动率改为0.8倍
3. 待验证：下次测试验证优化效果
```

## 实盘前检查清单
- [ ] 所有单元测试通过
- [ ] 模拟测试连续运行72小时无崩溃
- [ ] 错误注入测试无错误交易产生
- [ ] 资金计算、盈亏计算100%准确
- [ ] 订单逻辑、持仓状态完全符合预期
- [ ] 性能指标满足实盘要求
- [ ] 极端行情测试通过
- [ ] 多日连续测试通过