package model

import "time"

// TickData 逐笔成交数据（对应 002484.csv 的每一行）
type TickData struct {
	TranID          int64   `json:"tran_id"`           // 成交编号
	Time            string  `json:"time"`              // 成交时间 HH:MM:SS
	Price           float64 `json:"price"`             // 成交价格
	Volume          int64   `json:"volume"`            // 成交量（股）
	SaleOrderVolume int64   `json:"sale_order_volume"` // 卖方委托量
	BuyOrderVolume  int64   `json:"buy_order_volume"`  // 买方委托量
	Type            string  `json:"type"`              // B=买入主导 S=卖出主导
	SaleOrderID     int64   `json:"sale_order_id"`     // 卖方委托编号
	SaleOrderPrice  float64 `json:"sale_order_price"`  // 卖方委托价格
	BuyOrderID      int64   `json:"buy_order_id"`      // 买方委托编号
	BuyOrderPrice   float64 `json:"buy_order_price"`   // 买方委托价格
}

// TickBar 聚合后的 K 线级别数据（按固定时间窗口聚合 tick）
type TickBar struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	Volume     int64   `json:"volume"`      // 总成交量
	Amount     float64 `json:"amount"`      // 总成交额
	TradeCount int     `json:"trade_count"` // 成交笔数
	BuyVolume  int64   `json:"buy_volume"`  // 主买量
	SellVolume int64   `json:"sell_volume"` // 主卖量
	VWAP       float64 `json:"vwap"`        // 成交量加权均价
}

// TrendPhase 趋势阶段标注
type TrendPhase int

const (
	TrendUnknown TrendPhase = iota
	TrendRising             // 上涨阶段
	TrendPeak               // 高点附近
	TrendFalling            // 下跌阶段
	TrendTrough             // 低点附近
)

func (t TrendPhase) String() string {
	switch t {
	case TrendRising:
		return "rising"
	case TrendPeak:
		return "peak"
	case TrendFalling:
		return "falling"
	case TrendTrough:
		return "trough"
	default:
		return "unknown"
	}
}

// TradeLabel 训练标签：用于标注每个时间点的理想交易动作
type TradeLabel struct {
	Time       string     `json:"time"`
	Phase      TrendPhase `json:"phase"`      // 当前趋势阶段
	PriceDir   float64    `json:"price_dir"`  // 未来价格变化方向 (+1/-1)
	PriceChg   float64    `json:"price_chg"`  // 未来价格变化幅度（%）
	Action     int        `json:"action"`     // 0=持仓观望 1=买入 2=卖出
	Confidence float64    `json:"confidence"` // 标签置信度
}

// Feature 模型输入特征向量
type Feature struct {
	Date     string     `json:"date"`   // 交易日期 YYYY-MM-DD
	Symbol   string     `json:"symbol"` // 标的代码
	Time     string     `json:"time"`
	Values   []float64  `json:"values"`    // 特征值数组
	Label    int        `json:"label"`     // 目标标签：0=跌 1=涨
	PriceChg float64    `json:"price_chg"` // 价格变化百分比
	Phase    TrendPhase `json:"phase"`
}

// DayTicks 单日 tick 数据集合
type DayTicks struct {
	Date   string     `json:"date"`   // YYYY-MM-DD
	Symbol string     `json:"symbol"` // 标的代码
	Ticks  []TickData `json:"ticks"`
}

// MultiDayData 多日数据聚合结果
type MultiDayData struct {
	Symbol   string               `json:"symbol"`
	Days     []string             `json:"days"`     // 按日期排序的日期列表
	DayBars  map[string][]TickBar `json:"day_bars"` // date -> bars
	AllBars  []TickBar            `json:"all_bars"` // 全量 bars（按时间排序）
	Features []Feature            `json:"features"`
	Stats    []DailyStats         `json:"stats"` // 每日统计
}

// PredictionResult 模型预测结果
type PredictionResult struct {
	Time        string  `json:"time"`
	Symbol      string  `json:"symbol"`
	RiseProb    float64 `json:"rise_prob"`    // 上涨概率
	FallProb    float64 `json:"fall_prob"`    // 下跌概率
	Action      string  `json:"action"`       // buy/sell/hold
	Confidence  float64 `json:"confidence"`   // 预测置信度
	ExpectedPnL float64 `json:"expected_pnl"` // 预期收益
}

// BacktestResult 回测结果
type BacktestResult struct {
	Symbol      string        `json:"symbol"`
	StartTime   string        `json:"start_time"`
	EndTime     string        `json:"end_time"`
	InitialCash float64       `json:"initial_cash"`
	FinalValue  float64       `json:"final_value"`
	TotalReturn float64       `json:"total_return"` // 总收益率
	WinRate     float64       `json:"win_rate"`     // 胜率
	MaxDrawdown float64       `json:"max_drawdown"` // 最大回撤
	SharpeRatio float64       `json:"sharpe_ratio"` // 夏普比率
	TradeCount  int           `json:"trade_count"`  // 交易次数
	Trades      []TradeRecord `json:"trades"`       // 交易记录
}

// TradeRecord 单笔交易记录
type TradeRecord struct {
	EntryTime  string  `json:"entry_time"`
	ExitTime   string  `json:"exit_time"`
	Side       string  `json:"side"` // buy / sell
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
	Volume     int64   `json:"volume"`
	PnL        float64 `json:"pnl"`       // 盈亏金额
	PnLPct     float64 `json:"pnl_pct"`   // 盈亏百分比
	HoldBars   int     `json:"hold_bars"` // 持仓K线数
}

// ==================== 实时交易信号系统 ====================

// TradeSignal 交易信号（由实时推理引擎生成）
type TradeSignal struct {
	Date           string  `json:"date"`             // 交易日期
	Time           string  `json:"time"`             // 信号时间 HH:MM:SS
	Symbol         string  `json:"symbol"`           // 标的代码
	Action         string  `json:"action"`           // buy / sell / hold
	RiseProb       float64 `json:"rise_prob"`        // 上涨概率
	FallProb       float64 `json:"fall_prob"`        // 下跌概率
	Confidence     float64 `json:"confidence"`       // 置信度
	CurrentPrice   float64 `json:"current_price"`    // 当前价格
	TargetPrice    float64 `json:"target_price"`     // 目标价格
	StopLossPrice  float64 `json:"stop_loss_price"`  // 止损价格
	Volume         int64   `json:"volume"`           // 建议交易量
	WinRate        float64 `json:"win_rate"`         // 预估胜率
	OddsRatio      float64 `json:"odds_ratio"`       // 赔率（盈亏比）
	ProfitRate     float64 `json:"profit_rate"`      // 预期利润率 (%)
	ExpectedProfit float64 `json:"expected_profit"`  // 预期利润金额
	HoldBars       int     `json:"hold_bars"`        // 建议持有bar数
	HoldSeconds    int     `json:"hold_seconds"`     // 建议持有秒数
	ExpectSellTime string  `json:"expect_sell_time"` // 预期卖出时间
	KellyFraction  float64 `json:"kelly_fraction"`   // Kelly 最优仓位比例
	Phase          string  `json:"phase"`            // 当前趋势阶段
}

// Position 当前持仓状态
type Position struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // long / flat
	EntryTime  string  `json:"entry_time"`
	EntryPrice float64 `json:"entry_price"`
	Volume     int64   `json:"volume"`
	AvgCost    float64 `json:"avg_cost"`
	UnrealPnL  float64 `json:"unreal_pnl"` // 浮动盈亏
	UnrealPct  float64 `json:"unreal_pct"` // 浮动盈亏 %
	HoldBars   int     `json:"hold_bars"`
}

// PipelineState 闭环流水线状态
type PipelineState struct {
	Symbol        string        `json:"symbol"`
	Phase         string        `json:"phase"`          // train / infer / retrain
	TrainDays     []string      `json:"train_days"`     // 用于训练的日期
	InferDays     []string      `json:"infer_days"`     // 用于推理的日期
	CurrentDay    string        `json:"current_day"`    // 当前处理日期
	ModelVersion  int           `json:"model_version"`  // 模型版本号
	Position      Position      `json:"position"`       // 当前持仓
	Signals       []TradeSignal `json:"signals"`        // 历史信号
	Trades        []TradeRecord `json:"trades"`         // 已完成交易
	CumulativePnL float64       `json:"cumulative_pnl"` // 累计盈亏
	Cash          float64       `json:"cash"`           // 可用资金
	TotalValue    float64       `json:"total_value"`    // 总资产
	DailyResults  []DayResult   `json:"daily_results"`  // 每日结果汇总
}

// DayResult 单日推理+交易结果
type DayResult struct {
	Date        string        `json:"date"`
	SignalCount int           `json:"signal_count"`
	BuySignals  int           `json:"buy_signals"`
	SellSignals int           `json:"sell_signals"`
	TradeCount  int           `json:"trade_count"`
	DayPnL      float64       `json:"day_pnl"`
	DayReturn   float64       `json:"day_return"` // 当日收益率 %
	WinRate     float64       `json:"win_rate"`
	Trades      []TradeRecord `json:"trades"`
}

// DailyStats 日度统计汇总
type DailyStats struct {
	Date        time.Time `json:"date"`
	Symbol      string    `json:"symbol"`
	TotalTicks  int       `json:"total_ticks"`
	HighPrice   float64   `json:"high_price"`
	LowPrice    float64   `json:"low_price"`
	OpenPrice   float64   `json:"open_price"`
	ClosePrice  float64   `json:"close_price"`
	TotalVolume int64     `json:"total_volume"`
	HighTime    string    `json:"high_time"` // 最高价出现时间
	LowTime     string    `json:"low_time"`  // 最低价出现时间
	Amplitude   float64   `json:"amplitude"` // 振幅
}
