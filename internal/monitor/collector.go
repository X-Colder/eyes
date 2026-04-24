package monitor

import (
	"sync"
	"time"
)

// MonitorCollector 监控数据收集器
type MonitorCollector struct {
	// 训练监控
	trainingMetrics *TrainingMetrics
	trainingHistory []*TrainingSnapshot

	// 交易监控
	tradingMetrics *TradingMetrics
	recentSignals  []*SignalRecord
	recentTrades   []*TradeRecord

	// 收益监控
	returnMetrics *ReturnMetrics
	equityCurve   []*EquityPoint
	dailyReturns  []*DailyReturn

	// 风险监控
	riskMetrics    *RiskMetrics
	riskHistory    []*RiskSnapshot
	correlationMap map[string]map[string]float64

	// 系统状态
	systemStatus *SystemStatus

	mu sync.RWMutex
}

// NewMonitorCollector 创建监控收集器
func NewMonitorCollector() *MonitorCollector {
	return &MonitorCollector{
		trainingMetrics: &TrainingMetrics{},
		trainingHistory: make([]*TrainingSnapshot, 0, 1000),

		tradingMetrics: &TradingMetrics{},
		recentSignals:  make([]*SignalRecord, 0, 100),
		recentTrades:   make([]*TradeRecord, 0, 100),

		returnMetrics: &ReturnMetrics{},
		equityCurve:   make([]*EquityPoint, 0, 1000),
		dailyReturns:  make([]*DailyReturn, 0, 365),

		riskMetrics:    &RiskMetrics{},
		riskHistory:    make([]*RiskSnapshot, 0, 1000),
		correlationMap: make(map[string]map[string]float64),

		systemStatus: &SystemStatus{},
	}
}

// TrainingMetrics 训练指标
type TrainingMetrics struct {
	CurrentEpoch   int     `json:"current_epoch"`
	TotalEpochs    int     `json:"total_epochs"`
	TrainLoss      float64 `json:"train_loss"`
	ValLoss        float64 `json:"val_loss"`
	TrainAccuracy  float64 `json:"train_accuracy"`
	ValAccuracy    float64 `json:"val_accuracy"`
	LearningRate   float64 `json:"learning_rate"`
	BestValLoss    float64 `json:"best_val_loss"`
	BestEpoch      int     `json:"best_epoch"`
	TrainingTime   float64 `json:"training_time"` // 秒
	Progress       float64 `json:"progress"`      // 百分比
	Status         string  `json:"status"`        // running/completed/failed
	StartTime      string  `json:"start_time"`
	GPUUtilization float64 `json:"gpu_utilization"`
	MemoryUsage    float64 `json:"memory_usage"`
}

// TrainingSnapshot 训练快照
type TrainingSnapshot struct {
	Timestamp    string  `json:"timestamp"`
	Epoch        int     `json:"epoch"`
	TrainLoss    float64 `json:"train_loss"`
	ValLoss      float64 `json:"val_loss"`
	TrainAcc     float64 `json:"train_acc"`
	ValAcc       float64 `json:"val_acc"`
	LearningRate float64 `json:"learning_rate"`
}

// TradingMetrics 交易指标
type TradingMetrics struct {
	TotalSignals int64 `json:"total_signals"`
	BuySignals   int64 `json:"buy_signals"`
	SellSignals  int64 `json:"sell_signals"`
	HoldSignals  int64 `json:"hold_signals"`

	TotalTrades   int64   `json:"total_trades"`
	WinningTrades int64   `json:"winning_trades"`
	LosingTrades  int64   `json:"losing_trades"`
	WinRate       float64 `json:"win_rate"`

	OpenPositions int `json:"open_positions"`
	MaxPositions  int `json:"max_positions"`

	AvgHoldTime float64 `json:"avg_hold_time"` // 秒
	MaxHoldTime float64 `json:"max_hold_time"`
	MinHoldTime float64 `json:"min_hold_time"`

	TotalVolume   int64   `json:"total_volume"`
	TotalTurnover float64 `json:"total_turnover"`

	LatestSignalTime string `json:"latest_signal_time"`
	LatestTradeTime  string `json:"latest_trade_time"`
}

// SignalRecord 信号记录
type SignalRecord struct {
	Timestamp  string  `json:"timestamp"`
	Symbol     string  `json:"symbol"`
	Action     string  `json:"action"`
	Price      float64 `json:"price"`
	Volume     int64   `json:"volume"`
	Confidence float64 `json:"confidence"`
	Strategy   string  `json:"strategy"`
	Executed   bool    `json:"executed"`
}

// TradeRecord 交易记录
type TradeRecord struct {
	Timestamp  string  `json:"timestamp"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // buy/sell
	Price      float64 `json:"price"`
	Volume     int64   `json:"volume"`
	PnL        float64 `json:"pnl"`
	PnLPct     float64 `json:"pnl_pct"`
	HoldTime   float64 `json:"hold_time"`
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
}

// ReturnMetrics 收益指标
type ReturnMetrics struct {
	InitialCash   float64 `json:"initial_cash"`
	CurrentEquity float64 `json:"current_equity"`
	AvailableCash float64 `json:"available_cash"`

	TotalReturn   float64 `json:"total_return"`   // 总收益率
	DailyReturn   float64 `json:"daily_return"`   // 日收益率
	WeeklyReturn  float64 `json:"weekly_return"`  // 周收益率
	MonthlyReturn float64 `json:"monthly_return"` // 月收益率

	AnnualizedReturn float64 `json:"annualized_return"` // 年化收益

	TotalPnL      float64 `json:"total_pnl"`      // 总盈亏
	RealizedPnL   float64 `json:"realized_pnl"`   // 已实现盈亏
	UnrealizedPnL float64 `json:"unrealized_pnl"` // 未实现盈亏

	MaxDrawdown     float64 `json:"max_drawdown"`     // 最大回撤
	CurrentDrawdown float64 `json:"current_drawdown"` // 当前回撤

	SharpeRatio  float64 `json:"sharpe_ratio"`  // 夏普比率
	SortinoRatio float64 `json:"sortino_ratio"` // 索提诺比率
	CalmarRatio  float64 `json:"calmar_ratio"`  // 卡玛比率

	WinRate      float64 `json:"win_rate"`
	AvgWin       float64 `json:"avg_win"`
	AvgLoss      float64 `json:"avg_loss"`
	ProfitFactor float64 `json:"profit_factor"` // 盈亏比
}

// EquityPoint 权益点
type EquityPoint struct {
	Timestamp string  `json:"timestamp"`
	Equity    float64 `json:"equity"`
	Cash      float64 `json:"cash"`
	Position  float64 `json:"position"`
	Drawdown  float64 `json:"drawdown"`
}

// DailyReturn 日收益
type DailyReturn struct {
	Date   string  `json:"date"`
	Return float64 `json:"return"`
	PnL    float64 `json:"pnl"`
	Trades int     `json:"trades"`
}

// RiskMetrics 风险指标
type RiskMetrics struct {
	TotalRisk         float64 `json:"total_risk"`         // 总风险敞口
	MarketRisk        float64 `json:"market_risk"`        // 市场风险
	IdiosyncraticRisk float64 `json:"idiosyncratic_risk"` // 特质风险

	Leverage        float64 `json:"leverage"`         // 杠杆倍数
	MarginUsed      float64 `json:"margin_used"`      // 已用保证金
	MarginAvailable float64 `json:"margin_available"` // 可用保证金
	MarginRatio     float64 `json:"margin_ratio"`     // 保证金比例

	VaR95 float64 `json:"var_95"` // 95% VaR
	VaR99 float64 `json:"var_99"` // 99% VaR
	CVaR  float64 `json:"cvar"`   // 条件VaR

	Beta       float64 `json:"beta"`       // Beta系数
	Volatility float64 `json:"volatility"` // 波动率

	ConcentrationRisk float64 `json:"concentration_risk"` // 集中度风险
	CorrelationRisk   float64 `json:"correlation_risk"`   // 相关性风险
	Diversification   float64 `json:"diversification"`    // 分散化度

	RiskLimitUsage float64 `json:"risk_limit_usage"` // 风险限额使用率
}

// RiskSnapshot 风险快照
type RiskSnapshot struct {
	Timestamp  string  `json:"timestamp"`
	TotalRisk  float64 `json:"total_risk"`
	VaR95      float64 `json:"var_95"`
	MarginUsed float64 `json:"margin_used"`
	Leverage   float64 `json:"leverage"`
}

// SystemStatus 系统状态
type SystemStatus struct {
	Status    string `json:"status"` // running/stopped/error
	Uptime    int64  `json:"uptime"` // 运行时长(秒)
	StartTime string `json:"start_time"`

	ModelLoaded     bool   `json:"model_loaded"`
	ModelVersion    int    `json:"model_version"`
	ModelLastUpdate string `json:"model_last_update"`

	DataFeedStatus string `json:"data_feed_status"` // connected/disconnected
	LastDataTime   string `json:"last_data_time"`

	OrdersPending  int   `json:"orders_pending"`
	OrdersFilled   int64 `json:"orders_filled"`
	OrdersRejected int64 `json:"orders_rejected"`

	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`

	GoRoutines int `json:"goroutines"`
}

// UpdateTrainingMetrics 更新训练指标
func (mc *MonitorCollector) UpdateTrainingMetrics(metrics *TrainingMetrics) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.trainingMetrics = metrics

	// 记录快照
	snapshot := &TrainingSnapshot{
		Timestamp:    time.Now().Format("2006-01-02 15:04:05"),
		Epoch:        metrics.CurrentEpoch,
		TrainLoss:    metrics.TrainLoss,
		ValLoss:      metrics.ValLoss,
		TrainAcc:     metrics.TrainAccuracy,
		ValAcc:       metrics.ValAccuracy,
		LearningRate: metrics.LearningRate,
	}

	mc.trainingHistory = append(mc.trainingHistory, snapshot)

	// 限制历史记录数量
	if len(mc.trainingHistory) > 1000 {
		mc.trainingHistory = mc.trainingHistory[len(mc.trainingHistory)-1000:]
	}
}

// RecordSignal 记录信号
func (mc *MonitorCollector) RecordSignal(signal *SignalRecord) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.recentSignals = append(mc.recentSignals, signal)

	// 更新统计
	mc.tradingMetrics.TotalSignals++
	if signal.Action == "buy" {
		mc.tradingMetrics.BuySignals++
	} else if signal.Action == "sell" {
		mc.tradingMetrics.SellSignals++
	} else {
		mc.tradingMetrics.HoldSignals++
	}
	mc.tradingMetrics.LatestSignalTime = signal.Timestamp

	// 保留最近100条
	if len(mc.recentSignals) > 100 {
		mc.recentSignals = mc.recentSignals[len(mc.recentSignals)-100:]
	}
}

// RecordTrade 记录交易
func (mc *MonitorCollector) RecordTrade(trade *TradeRecord) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.recentTrades = append(mc.recentTrades, trade)

	// 更新统计
	mc.tradingMetrics.TotalTrades++
	if trade.PnL > 0 {
		mc.tradingMetrics.WinningTrades++
	} else {
		mc.tradingMetrics.LosingTrades++
	}
	mc.tradingMetrics.WinRate = float64(mc.tradingMetrics.WinningTrades) /
		float64(mc.tradingMetrics.TotalTrades)
	mc.tradingMetrics.LatestTradeTime = trade.Timestamp

	// 保留最近100条
	if len(mc.recentTrades) > 100 {
		mc.recentTrades = mc.recentTrades[len(mc.recentTrades)-100:]
	}
}

// UpdateEquity 更新权益曲线
func (mc *MonitorCollector) UpdateEquity(equity, cash, position float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05")

	drawdown := 0.0
	if len(mc.equityCurve) > 0 {
		peak := mc.equityCurve[0].Equity
		for _, p := range mc.equityCurve {
			if p.Equity > peak {
				peak = p.Equity
			}
		}
		if peak > 0 {
			drawdown = (peak - equity) / peak
		}
	}

	point := &EquityPoint{
		Timestamp: now,
		Equity:    equity,
		Cash:      cash,
		Position:  position,
		Drawdown:  drawdown,
	}

	mc.equityCurve = append(mc.equityCurve, point)

	// 更新收益指标
	mc.returnMetrics.CurrentEquity = equity
	mc.returnMetrics.AvailableCash = cash
	mc.returnMetrics.UnrealizedPnL = position
	mc.returnMetrics.TotalReturn = (equity - mc.returnMetrics.InitialCash) /
		mc.returnMetrics.InitialCash * 100
	mc.returnMetrics.CurrentDrawdown = drawdown

	if drawdown > mc.returnMetrics.MaxDrawdown {
		mc.returnMetrics.MaxDrawdown = drawdown
	}

	// 限制历史记录
	if len(mc.equityCurve) > 1000 {
		mc.equityCurve = mc.equityCurve[len(mc.equityCurve)-1000:]
	}
}

// UpdateRiskMetrics 更新风险指标
func (mc *MonitorCollector) UpdateRiskMetrics(metrics *RiskMetrics) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.riskMetrics = metrics

	snapshot := &RiskSnapshot{
		Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
		TotalRisk:  metrics.TotalRisk,
		VaR95:      metrics.VaR95,
		MarginUsed: metrics.MarginUsed,
		Leverage:   metrics.Leverage,
	}

	mc.riskHistory = append(mc.riskHistory, snapshot)

	if len(mc.riskHistory) > 1000 {
		mc.riskHistory = mc.riskHistory[len(mc.riskHistory)-1000:]
	}
}

// UpdateCorrelations 更新相关性矩阵
func (mc *MonitorCollector) UpdateCorrelations(corr map[string]map[string]float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.correlationMap = corr
}

// UpdateSystemStatus 更新系统状态
func (mc *MonitorCollector) UpdateSystemStatus(status *SystemStatus) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.systemStatus = status
}

// GetDashboardData 获取仪表盘数据
func (mc *MonitorCollector) GetDashboardData() *DashboardData {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return &DashboardData{
		Training:      mc.trainingMetrics,
		TrainingHist:  mc.trainingHistory,
		Trading:       mc.tradingMetrics,
		RecentSignals: mc.recentSignals,
		RecentTrades:  mc.recentTrades,
		Returns:       mc.returnMetrics,
		EquityCurve:   mc.equityCurve,
		DailyReturns:  mc.dailyReturns,
		Risk:          mc.riskMetrics,
		RiskHist:      mc.riskHistory,
		Correlations:  mc.correlationMap,
		System:        mc.systemStatus,
		Timestamp:     time.Now().Format("2006-01-02 15:04:05"),
	}
}

// DashboardData 仪表盘数据
type DashboardData struct {
	Training     *TrainingMetrics    `json:"training"`
	TrainingHist []*TrainingSnapshot `json:"training_history"`

	Trading       *TradingMetrics `json:"trading"`
	RecentSignals []*SignalRecord `json:"recent_signals"`
	RecentTrades  []*TradeRecord  `json:"recent_trades"`

	Returns      *ReturnMetrics `json:"returns"`
	EquityCurve  []*EquityPoint `json:"equity_curve"`
	DailyReturns []*DailyReturn `json:"daily_returns"`

	Risk         *RiskMetrics                  `json:"risk"`
	RiskHist     []*RiskSnapshot               `json:"risk_history"`
	Correlations map[string]map[string]float64 `json:"correlations"`

	System    *SystemStatus `json:"system"`
	Timestamp string        `json:"timestamp"`
}
