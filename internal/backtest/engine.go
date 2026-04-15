package backtest

import (
	"log"
	"math"

	"github.com/eyes/internal/model"
)

// Engine 回测引擎
type Engine struct {
	InitialCash float64
	Commission  float64 // 手续费率 (如 0.0003)
	Slippage    float64 // 滑点 (如 0.001)
	MaxPosition int64   // 最大持仓量

	cash       float64
	position   int64   // 当前持仓
	avgCost    float64 // 持仓均价
	trades     []model.TradeRecord
	equity     []float64 // 每个bar的权益
	peakEquity float64
	maxDD      float64
}

// NewEngine 创建回测引擎
func NewEngine(initialCash, commission, slippage float64, maxPosition int64) *Engine {
	return &Engine{
		InitialCash: initialCash,
		Commission:  commission,
		Slippage:    slippage,
		MaxPosition: maxPosition,
	}
}

// Run 基于预测信号执行回测
// predictions: 每根 bar 对应的预测结果
// bars: 对应的 bar 数据
func (e *Engine) Run(bars []model.TickBar, predictions []model.PredictionResult) model.BacktestResult {
	e.reset()

	n := min(len(bars), len(predictions))
	if n == 0 {
		return model.BacktestResult{}
	}

	log.Printf("[backtest] starting backtest: %d bars, cash=%.2f", n, e.InitialCash)

	for i := 0; i < n; i++ {
		bar := bars[i]
		pred := predictions[i]
		price := bar.Close

		switch pred.Action {
		case "buy":
			if e.position == 0 && pred.Confidence > 0.5 {
				e.buy(price, bar.EndTime)
			}
		case "sell":
			if e.position > 0 && pred.Confidence > 0.5 {
				e.sell(price, bar.EndTime)
			}
		}

		// 记录权益
		equity := e.cash + float64(e.position)*price
		e.equity = append(e.equity, equity)

		// 更新最大回撤
		if equity > e.peakEquity {
			e.peakEquity = equity
		}
		if e.peakEquity > 0 {
			dd := (e.peakEquity - equity) / e.peakEquity
			if dd > e.maxDD {
				e.maxDD = dd
			}
		}
	}

	// 如果最后还有持仓，强制平仓
	if e.position > 0 {
		lastPrice := bars[n-1].Close
		e.sell(lastPrice, bars[n-1].EndTime)
	}

	return e.buildResult(bars[0].StartTime, bars[n-1].EndTime)
}

// RunWithLabels 基于理想交易标签执行回测（衡量完美策略上限）
func (e *Engine) RunWithLabels(bars []model.TickBar, labels []model.TradeLabel) model.BacktestResult {
	e.reset()

	n := min(len(bars), len(labels))
	if n == 0 {
		return model.BacktestResult{}
	}

	for i := 0; i < n; i++ {
		price := bars[i].Close
		label := labels[i]

		switch label.Action {
		case 1: // 买入
			if e.position == 0 {
				e.buy(price, bars[i].EndTime)
			}
		case 2: // 卖出
			if e.position > 0 {
				e.sell(price, bars[i].EndTime)
			}
		}

		equity := e.cash + float64(e.position)*price
		e.equity = append(e.equity, equity)
		if equity > e.peakEquity {
			e.peakEquity = equity
		}
		if e.peakEquity > 0 {
			dd := (e.peakEquity - equity) / e.peakEquity
			if dd > e.maxDD {
				e.maxDD = dd
			}
		}
	}

	if e.position > 0 {
		e.sell(bars[n-1].Close, bars[n-1].EndTime)
	}

	return e.buildResult(bars[0].StartTime, bars[n-1].EndTime)
}

func (e *Engine) reset() {
	e.cash = e.InitialCash
	e.position = 0
	e.avgCost = 0
	e.trades = nil
	e.equity = nil
	e.peakEquity = e.InitialCash
	e.maxDD = 0
}

func (e *Engine) buy(price float64, timeStr string) {
	execPrice := price * (1 + e.Slippage)
	// 用可用资金的 90% 买入
	availCash := e.cash * 0.9
	qty := int64(availCash / (execPrice * (1 + e.Commission)))
	if qty <= 0 {
		return
	}
	if qty > e.MaxPosition {
		qty = e.MaxPosition
	}

	cost := float64(qty) * execPrice * (1 + e.Commission)
	e.cash -= cost
	e.avgCost = execPrice
	e.position = qty

	e.trades = append(e.trades, model.TradeRecord{
		EntryTime:  timeStr,
		Side:       "buy",
		EntryPrice: execPrice,
		Volume:     qty,
	})
}

func (e *Engine) sell(price float64, timeStr string) {
	if e.position <= 0 {
		return
	}
	execPrice := price * (1 - e.Slippage)
	proceeds := float64(e.position) * execPrice * (1 - e.Commission)
	e.cash += proceeds

	pnl := (execPrice - e.avgCost) * float64(e.position)
	pnlPct := 0.0
	if e.avgCost > 0 {
		pnlPct = (execPrice - e.avgCost) / e.avgCost * 100
	}

	// 更新最后一条买入记录
	if len(e.trades) > 0 {
		last := &e.trades[len(e.trades)-1]
		if last.ExitTime == "" {
			last.ExitTime = timeStr
			last.ExitPrice = execPrice
			last.PnL = pnl
			last.PnLPct = pnlPct
		}
	}

	e.position = 0
	e.avgCost = 0
}

func (e *Engine) buildResult(startTime, endTime string) model.BacktestResult {
	finalValue := e.cash
	totalReturn := 0.0
	if e.InitialCash > 0 {
		totalReturn = (finalValue - e.InitialCash) / e.InitialCash * 100
	}

	// 统计胜率
	wins := 0
	completedTrades := 0
	for _, t := range e.trades {
		if t.ExitTime != "" {
			completedTrades++
			if t.PnL > 0 {
				wins++
			}
		}
	}
	winRate := 0.0
	if completedTrades > 0 {
		winRate = float64(wins) / float64(completedTrades) * 100
	}

	// 计算夏普比率
	sharpe := e.calcSharpe()

	result := model.BacktestResult{
		StartTime:   startTime,
		EndTime:     endTime,
		InitialCash: e.InitialCash,
		FinalValue:  finalValue,
		TotalReturn: totalReturn,
		WinRate:     winRate,
		MaxDrawdown: e.maxDD * 100,
		SharpeRatio: sharpe,
		TradeCount:  completedTrades,
		Trades:      e.trades,
	}

	log.Printf("[backtest] result: return=%.2f%% winRate=%.1f%% maxDD=%.2f%% trades=%d sharpe=%.2f",
		totalReturn, winRate, e.maxDD*100, completedTrades, sharpe)

	return result
}

func (e *Engine) calcSharpe() float64 {
	if len(e.equity) < 2 {
		return 0
	}
	returns := make([]float64, len(e.equity)-1)
	for i := 1; i < len(e.equity); i++ {
		if e.equity[i-1] > 0 {
			returns[i-1] = (e.equity[i] - e.equity[i-1]) / e.equity[i-1]
		}
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		d := r - avg
		variance += d * d
	}
	variance /= float64(len(returns) - 1)
	std := math.Sqrt(variance)

	if std == 0 {
		return 0
	}
	// 年化（假设一天 ~480 个 30s bar）
	return avg / std * math.Sqrt(480)
}
