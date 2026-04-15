# Eyes 量化交易系统 - 测试报告

**生成日期**: 2026-04-15  
**Go 版本**: 1.24.4  
**运行环境**: macOS darwin 15.6.1

---

## 一、测试概览

| 指标 | 数值 |
|------|------|
| 测试模块数 | 7 |
| 测试用例总数 | 36 |
| 通过数 | 36 |
| 失败数 | 0 |
| 总覆盖率 | 53.5% |

## 二、各模块测试详情

### 2.1 model（数据模型）- 覆盖率 100%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestTrendPhaseString | 趋势阶段枚举字符串化 | PASS |
| TestTickDataJSON | TickData JSON 序列化/反序列化 | PASS |
| TestTradeSignalJSON | TradeSignal JSON 往返一致性 | PASS |
| TestPositionJSON | Position JSON 往返一致性 | PASS |
| TestPipelineStateInit | PipelineState 初始化零值 | PASS |

### 2.2 config（配置加载）- 覆盖率 100%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestLoadValid | 完整配置文件加载（含 pipeline 段） | PASS |
| TestLoadMissing | 文件不存在时返回错误 | PASS |
| TestLoadInvalidJSON | 非法 JSON 格式返回错误 | PASS |
| TestLoadPartial | 部分配置字段缺省降级 | PASS |

### 2.3 loader（数据加载）- 覆盖率 82.0%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestLoadTickCSV | 加载 4 行 tick CSV 并验证排序和字段 | PASS |
| TestLoadTickCSVMissing | 文件不存在时返回错误 | PASS |
| TestLoadTickCSVSkipBadRows | 列数不一致行自动跳过 | PASS |
| TestParseFileName | 6 种文件名模式解析（含日期/纯代码/无效） | PASS |
| TestLoadMultiDayDir | 多日目录加载并按日期排序 | PASS |
| TestLoadMultiDayDirFilterSymbol | 按标的代码过滤文件 | PASS |
| TestGetDailyStats | 日度统计（OHLC/振幅/成交量） | PASS |

### 2.4 feature（特征工程）- 覆盖率 87.4%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestAggregateBars | 500 笔 tick 聚合为 30s bar，验证 OHLCV | PASS |
| TestAggregateBarsEmpty | 空 tick 返回空 bar | PASS |
| TestExtractFeatures | 特征维度 = 14×10+10 = 150，标签合法性 | PASS |
| TestExtractFeaturesNotEnoughBars | bar 不足时返回空特征 | PASS |
| TestExtractFeaturesWithMeta | 特征附加 date/symbol 元信息 | PASS |
| TestExportFeaturesCSV | 导出 CSV 文件可读 | PASS |
| TestExportFeaturesCSVEmpty | 空特征导出返回错误 | PASS |

### 2.5 analysis（趋势分析）- 覆盖率 95.7%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestIdentifyPhases | W 型走势识别 rising/falling 阶段 | PASS |
| TestIdentifyPhasesEmpty | 空 bar 返回 nil | PASS |
| TestLabelFeatures | 为特征标注趋势阶段 | PASS |
| TestGenerateTradeLabels | 生成买入/卖出/持仓标签 | PASS |
| TestNewTrendAnalyzer | 构造参数验证 | PASS |

### 2.6 backtest（回测引擎）- 覆盖率 94.4%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestNewEngine | 构造参数验证 | PASS |
| TestRunWithPredictions | 基于预测信号回测（低买高卖盈利） | PASS |
| TestRunWithLabels | 基于理想标签回测 | PASS |
| TestRunEmptyBars | 空数据回测返回零交易 | PASS |
| TestForceCloseAtEnd | 尾盘未平仓自动强制平仓 | PASS |
| TestMaxDrawdown | 最大回撤非负验证 | PASS |
| TestWinRate | 胜率范围 [0, 100] 验证 | PASS |

### 2.7 engine（信号引擎 + 闭环编排）- 覆盖率 17.8%

| 用例 | 说明 | 结果 |
|------|------|------|
| TestNewSignalEngine | 信号引擎构造与初始资金 | PASS |
| TestGetState | 初始状态（flat/100000/空交易） | PASS |
| TestForceCloseNoPosition | 无持仓时强制平仓返回 nil | PASS |
| TestForceCloseWithPosition | 有持仓强制平仓收益计算正确 | PASS |
| TestEstimateWinRate | 胜率估算范围 [0.5, 1.0] | PASS |
| TestCalcVolatility | 波动率计算（正常窗口/单 bar 默认值） | PASS |
| TestEstimateAvgWinLoss | 平均盈亏估算 > 0 | PASS |
| TestNewPipeline | 闭环流水线初始化状态验证 | PASS |

> **注**: engine 模块覆盖率较低因为 `ProcessDayTicks`、`callPredict`、`Pipeline.Run` 等核心方法依赖外部 Python 推理服务，属于集成测试范畴，需在完整环境下运行。

## 三、覆盖率说明

| 模块 | 覆盖率 | 说明 |
|------|--------|------|
| model | 100% | 纯数据结构，完整覆盖 |
| config | 100% | 配置加载逻辑完整覆盖 |
| analysis | 95.7% | 趋势分析核心逻辑覆盖 |
| backtest | 94.4% | 回测引擎核心逻辑覆盖 |
| feature | 87.4% | 特征工程核心逻辑覆盖 |
| loader | 82.0% | CSV 解析/多日加载覆盖 |
| engine | 17.8% | 依赖推理服务的集成逻辑未覆盖 |
| api | 0% | HTTP 处理器需集成测试 |

## 四、运行命令

```bash
# 运行全部测试
make test

# 带覆盖率
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out

# 带详细输出
go test ./... -v -count=1

# 单模块测试
go test ./internal/feature/ -v
```