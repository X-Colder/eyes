package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/eyes/internal/analysis"
	"github.com/eyes/internal/config"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/loader"
	"github.com/eyes/internal/model"
	"github.com/eyes/internal/monitor"
)

// SimTrader 模拟交易器（多因子预测，不依赖 Python 服务）
type SimTrader struct {
	featureEng *feature.Engineer
	trendAna   *analysis.TrendAnalyzer
	bars       []model.TickBar
	cash       float64
	position   model.Position
	commission float64
	slippage   float64
	maxPos     int64
	mu         sync.Mutex

	// 移动止盈
	highestPrice float64 // 持仓期间最高价

	// 统计
	totalSignals  int
	buySignals    int
	sellSignals   int
	holdSignals   int
	totalTrades   int
	winningTrades int

	// 交易控制
	cooldownBars  int    // 卖出后冷却期（剩余bar数）
	dayBuyCount   int    // 当天买入次数
	lastTradeDate string // 上次交易日期
}

func NewSimTrader(cfg *config.Config) *SimTrader {
	return &SimTrader{
		featureEng: feature.NewEngineer(
			cfg.Feature.BarInterval,
			cfg.Feature.WindowSize,
			cfg.Feature.FutureSteps,
			cfg.Feature.PriceThresh,
		),
		trendAna:   analysis.NewTrendAnalyzer(5, 0.02),
		cash:       cfg.Backtest.InitialCash,
		commission: cfg.Backtest.Commission,
		slippage:   cfg.Backtest.Slippage,
		maxPos:     cfg.Backtest.MaxPosition,
		position:   model.Position{Side: "flat"},
	}
}

// ProcessDay 处理一天的数据，返回当天信号和交易
func (st *SimTrader) ProcessDay(date, symbol string, ticks []model.TickData,
	collector *monitor.MonitorCollector) ([]model.TradeSignal, []model.TradeRecord) {

	bars := st.featureEng.AggregateBars(ticks)
	if len(bars) == 0 {
		return nil, nil
	}

	// 日买入计数重置
	if date != st.lastTradeDate {
		st.dayBuyCount = 0
		st.lastTradeDate = date
	}

	var signals []model.TradeSignal
	var trades []model.TradeRecord

	// 获取趋势阶段
	phases := st.trendAna.IdentifyPhases(bars)

	for i, bar := range bars {
		st.bars = append(st.bars, bar)

		// 递减冷却期
		if st.cooldownBars > 0 {
			st.cooldownBars--
		}

		// 需要至少 windowSize 个 bar 才能预测
		if len(st.bars) < st.featureEng.WindowSize {
			continue
		}

		signal := st.generateSignal(date, bar, st.bars, phases, i, len(bars), symbol)
		st.totalSignals++
		switch signal.Action {
		case "buy":
			st.buySignals++
		case "sell":
			st.sellSignals++
		default:
			st.holdSignals++
		}

		signals = append(signals, signal)

		// 记录信号
		collector.RecordSignal(&monitor.SignalRecord{
			Timestamp:  fmt.Sprintf("%s %s", date, bar.EndTime),
			Symbol:     symbol,
			Action:     signal.Action,
			Price:      bar.Close,
			Volume:     signal.Volume,
			Confidence: signal.Confidence,
			Strategy:   "multiFactor",
			Executed:   signal.Volume > 0,
		})

		// 执行交易
		trade := st.executeTrade(signal, bar, i, len(bars), collector)
		if trade != nil {
			trades = append(trades, *trade)
		}
	}

	// 日终条件性平仓
	if st.position.Side == "long" {
		lastBar := bars[len(bars)-1]
		unrealPct := 0.0
		if st.position.AvgCost > 0 {
			unrealPct = (lastBar.Close - st.position.AvgCost) / st.position.AvgCost * 100
		}
		// 浮亏止损平仓
		if unrealPct < -0.5 {
			trade := st.forceClose(lastBar.Close, fmt.Sprintf("%s %s", date, lastBar.EndTime))
			if trade != nil {
				log.Printf("[eod] force close: unrealPct=%.2f%%", unrealPct)
				trades = append(trades, *trade)
			}
		} else if unrealPct >= 0 && unrealPct < 1.0 {
			// 微利锁利
			trade := st.forceClose(lastBar.Close, fmt.Sprintf("%s %s", date, lastBar.EndTime))
			if trade != nil {
				log.Printf("[eod] lock profit: unrealPct=%.2f%%", unrealPct)
				trades = append(trades, *trade)
			}
		}
		// 浮盈>=1%持仓过夜
	}

	return signals, trades
}

// multiFactorPredict 多因子综合预测器
func (st *SimTrader) multiFactorPredict(bar model.TickBar, bars []model.TickBar) (direction int, confidence float64) {
	featureScore := 0.0
	windowSize := 10

	// ========== 1. 买卖力度因子 ==========
	if len(bars) >= 3 {
		recentBars := bars[len(bars)-3:]
		avgBuyRatio := 0.0
		for _, b := range recentBars {
			totalVol := b.BuyVolume + b.SellVolume
			if totalVol > 0 {
				avgBuyRatio += float64(b.BuyVolume) / float64(totalVol)
			}
		}
		avgBuyRatio /= 3.0
		avgBuyScore := (avgBuyRatio - 0.5) * 2.0
		featureScore += avgBuyScore * 0.10
	}

	// ========== 2. RSI 因子（Wilder平滑，避免极端值） ==========
	rsiScore := 0.0
	rsi := st.calcRSI(bars, 14)
	if rsi >= 0 {
		if rsi < 30 {
			rsiScore = 0.5
		} else if rsi < 40 {
			rsiScore = 0.3
		} else if rsi > 70 {
			rsiScore = -0.5
		} else if rsi > 60 {
			rsiScore = -0.3
		}
	}
	featureScore += rsiScore * 0.15

	// ========== 3. MACD 因子 ==========
	macdScore := 0.0
	macdLine, signalLine, _ := st.calcMACD(bars)
	if !math.IsNaN(macdLine) && !math.IsNaN(signalLine) {
		diff := macdLine - signalLine
		macdScore = math.Max(-1, math.Min(1, diff*5))
	}
	featureScore += macdScore * 0.15

	// ========== 4. 布林带因子 ==========
	bollScore := 0.0
	upper, middle, lower := st.calcBollinger(bars, 20, 2.0)
	if middle > 0 && upper > lower {
		bandWidth := upper - lower
		if bandWidth > 0 {
			position := (bar.Close - lower) / bandWidth
			if position < 0.2 {
				bollScore = 0.4
			} else if position < 0.4 {
				bollScore = 0.2
			} else if position > 0.8 {
				bollScore = -0.4
			} else if position > 0.6 {
				bollScore = -0.2
			}
		}
	}
	featureScore += bollScore * 0.12

	// ========== 5. K线形态因子 ==========
	candleScore := 0.0
	if len(bars) >= 2 {
		prev := bars[len(bars)-2]
		body := bar.Close - bar.Open
		upperWick := bar.High - math.Max(bar.Open, bar.Close)
		lowerWick := math.Min(bar.Open, bar.Close) - bar.Low
		totalRange := bar.High - bar.Low

		if totalRange > 0 {
			if body > 0 && lowerWick/totalRange > 0.4 {
				candleScore = 0.3 // 锤子线看涨
			} else if body < 0 && upperWick/totalRange > 0.4 {
				candleScore = -0.3 // 射击之星看跌
			}
			if body > 0 && prev.Close < prev.Open && body > math.Abs(prev.Close-prev.Open) {
				candleScore += 0.4 // 看涨吞没
			} else if body < 0 && prev.Close > prev.Open && math.Abs(body) > (prev.Close-prev.Open) {
				candleScore -= 0.4 // 看跌吞没
			}
		}
	}
	featureScore += candleScore * 0.10

	// ========== 6. 窗口动量因子 ==========
	momentumScore := 0.0
	if len(bars) >= windowSize {
		windowBars := bars[len(bars)-windowSize:]
		startPrice := windowBars[0].Close
		if startPrice > 0 {
			momentum := (bar.Close - startPrice) / startPrice
			momentumScore = math.Max(-1, math.Min(1, momentum*20))
		}
	}
	featureScore += momentumScore * 0.13

	// ========== 7. 波动率因子 ==========
	volScore := 0.0
	if len(bars) >= windowSize {
		windowBars := bars[len(bars)-windowSize:]
		returns := make([]float64, len(windowBars)-1)
		for i := 1; i < len(windowBars); i++ {
			if windowBars[i-1].Close > 0 {
				returns[i-1] = (windowBars[i].Close - windowBars[i-1].Close) / windowBars[i-1].Close
			}
		}
		if len(returns) > 1 {
			mean := 0.0
			for _, r := range returns {
				mean += r
			}
			mean /= float64(len(returns))
			variance := 0.0
			for _, r := range returns {
				variance += (r - mean) * (r - mean)
			}
			variance /= float64(len(returns))
			volatility := math.Sqrt(variance)
			if volatility < 0.003 {
				volScore = 0.2 // 低波动，可能突破
			} else if volatility > 0.01 {
				volScore = -0.2 // 高波动，风险大
			}
		}
	}
	featureScore += volScore * 0.05

	// ========== 8. 量价配合因子 ==========
	volPriceScore := 0.0
	if len(bars) >= 3 {
		recent := bars[len(bars)-3:]
		priceUp := recent[2].Close > recent[1].Close
		volUp := recent[2].Volume > recent[1].Volume
		if priceUp && volUp {
			volPriceScore = 0.3 // 量价齐升
		} else if !priceUp && volUp {
			volPriceScore = -0.3 // 放量下跌
		} else if priceUp && !volUp {
			volPriceScore = -0.1 // 缩量上涨
		}
	}
	featureScore += volPriceScore * 0.10

	// ========== 9. 趋势因子 ==========
	trendScore := 0.0
	if len(bars) >= 20 {
		ma5 := st.calcSMA(bars, 5)
		ma20 := st.calcSMA(bars, 20)
		if ma20 > 0 {
			if ma5 > ma20 {
				trendScore = 0.3
			} else if ma5 < ma20 {
				trendScore = -0.3
			}
		}
	}
	featureScore += trendScore * 0.10

	// 综合评分：放大5倍后映射到0.5附近
	// 旧版: featureScore=0.332(放大后) => conf=0.616, 映射: conf=0.5+featureScore*0.35
	// 原始featureScore约0.01-0.10, 放大5×后0.05-0.50
	// buy需 conf>=0.60 => featureScore(放大后)>=0.286 => 原始>=0.057
	featureScore *= 5.0
	confidence = 0.5 + featureScore*0.35
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}
	if featureScore > 0 {
		direction = 1
	} else if featureScore < 0 {
		direction = -1
	} else {
		direction = 0
	}
	return
}

// calcSMA 简单移动平均
func (st *SimTrader) calcSMA(bars []model.TickBar, period int) float64 {
	if len(bars) < period {
		return 0
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

// generateSignal 生成交易信号
func (st *SimTrader) generateSignal(date string, bar model.TickBar, bars []model.TickBar,
	phases []model.TrendPhase, barIdx, totalBars int, symbol string) model.TradeSignal {

	direction, confidence := st.multiFactorPredict(bar, bars)

	// 获取趋势阶段
	phase := model.TrendUnknown
	if barIdx < len(phases) {
		phase = phases[barIdx]
	}
	// 阈值（confidence范围0-1，中心0.5）
	buyThreshold := 0.65
	sellThreshold := 0.60

	// ATR 止损止盈
	atr := st.calcATR(bars, 14)
	stopLossDist := atr * 2.5
	takeProfitDist := atr * 5.0
	stopLossPrice := bar.Close - stopLossDist
	takeProfitPrice := bar.Close + takeProfitDist

	// 趋势过滤：仓位系数
	trendMultiplier := 1.0
	if phase == model.TrendFalling {
		rsi := st.calcRSI(bars, 14)
		if rsi < 35 {
			trendMultiplier = 0.3 // 超卖可小仓抄底
		} else {
			trendMultiplier = 0.0 // 下跌趋势阻止买入
		}
	} else if phase == model.TrendPeak {
		trendMultiplier = 0.5
	}

	action := "hold"
	var volume int64 = 0

	if direction == 1 && confidence >= buyThreshold && trendMultiplier > 0 &&
		st.cooldownBars == 0 && st.dayBuyCount < 1 {
		action = "buy"
		winRate := 0.35 + confidence*0.5
		oddsRatio := takeProfitDist / stopLossDist
		if oddsRatio < 1 {
			oddsRatio = 1
		}

		// 半 Kelly 仓位
		kellyFrac := 0.0
		if winRate > 0 && oddsRatio > 0 {
			kellyFrac = (winRate*oddsRatio - (1 - winRate)) / oddsRatio * 0.5
		}
		if kellyFrac < 0 {
			kellyFrac = 0
		}
		kellyFrac *= trendMultiplier
		if kellyFrac > 0.15 {
			kellyFrac = 0.15
		}

		positionValue := st.cash * kellyFrac
		if bar.Close > 0 {
			volume = int64(positionValue/bar.Close/100) * 100
		}
	} else if direction == -1 && (confidence <= (1.0 - sellThreshold)) && st.position.Side == "long" {
		// confidence=0.5+featureScore*5, direction=-1时confidence<0.5
		// 卖出条件: confidence足够低(看跌信号强)
		action = "sell"
	}

	profitRate := 0.0
	if takeProfitDist > 0 && bar.Close > 0 {
		profitRate = takeProfitDist / bar.Close * 100
	}

	winRate := 0.35 + confidence*0.25
	oddsRatio := takeProfitDist / stopLossDist

	return model.TradeSignal{
		Date: date, Time: bar.EndTime, Symbol: symbol,
		Action: action, RiseProb: 0.5 + float64(direction)*0.2,
		FallProb: 0.5 - float64(direction)*0.2, Confidence: confidence,
		CurrentPrice: bar.Close, TargetPrice: takeProfitPrice, StopLossPrice: stopLossPrice,
		Volume: volume, WinRate: winRate, OddsRatio: oddsRatio,
		ProfitRate: profitRate, KellyFraction: 0,
	}
}

// executeTrade 执行交易（含止损止盈 + 移动止盈 + 条件卖出）
func (st *SimTrader) executeTrade(signal model.TradeSignal, bar model.TickBar, barIdx, totalBars int,
	collector *monitor.MonitorCollector) *model.TradeRecord {
	price := bar.Close

	// 止损止盈检查
	if st.position.Side == "long" {
		st.position.HoldBars++
		st.position.UnrealPnL = (price - st.position.AvgCost) * float64(st.position.Volume)
		if st.position.AvgCost > 0 {
			st.position.UnrealPct = (price - st.position.AvgCost) / st.position.AvgCost * 100
		}
		if price > st.highestPrice {
			st.highestPrice = price
		}

		shouldClose := false
		closeReason := ""

		if signal.StopLossPrice > 0 && price <= signal.StopLossPrice {
			shouldClose = true
			closeReason = "stoploss"
		}
		if signal.TargetPrice > 0 && price >= signal.TargetPrice {
			shouldClose = true
			closeReason = "takeprofit"
		}
		// 移动止盈
		if st.highestPrice > st.position.AvgCost && st.highestPrice > 0 {
			drawdown := (st.highestPrice - price) / st.highestPrice * 100
			unrealPct := (price - st.position.AvgCost) / st.position.AvgCost * 100
			if unrealPct > 1.0 && drawdown > 2.5 {
				shouldClose = true
				closeReason = "trailing-stop"
			}
		}
		if st.position.UnrealPct < -2.0 {
			shouldClose = true
			closeReason = "hardstop"
		}
		if st.position.HoldBars > 80 && st.position.UnrealPct < -0.3 {
			shouldClose = true
			closeReason = "timeout"
		}
		if st.position.HoldBars > 120 && st.position.UnrealPct > 0 && st.position.UnrealPct < 2.0 {
			shouldClose = true
			closeReason = "timeout-profit"
		}

		if shouldClose {
			execPrice := price * (1 - st.slippage)
			proceeds := float64(st.position.Volume) * execPrice * (1 - st.commission)
			st.cash += proceeds
			pnl := (execPrice - st.position.AvgCost) * float64(st.position.Volume)
			pnlPct := (execPrice - st.position.AvgCost) / st.position.AvgCost * 100

			trade := &model.TradeRecord{
				EntryTime: st.position.EntryTime, ExitTime: bar.EndTime, Side: "buy",
				EntryPrice: st.position.AvgCost, ExitPrice: execPrice,
				Volume: st.position.Volume, PnL: pnl, PnLPct: pnlPct, HoldBars: st.position.HoldBars,
			}
			st.totalTrades++
			if pnl > 0 {
				st.winningTrades++
			}
			log.Printf("[trade] SELL (%s): vol=%d entry=%.2f exit=%.2f pnl=%.2f pnlPct=%.2f%% holdBars=%d",
				closeReason, trade.Volume, trade.EntryPrice, trade.ExitPrice, pnl, pnlPct, trade.HoldBars)

			collector.RecordTrade(&monitor.TradeRecord{
				Timestamp: fmt.Sprintf("%s %s", signal.Date, bar.EndTime), Symbol: signal.Symbol,
				Side: "sell", Price: execPrice, Volume: trade.Volume, PnL: pnl, PnLPct: pnlPct,
				HoldTime:   float64(trade.HoldBars) * float64(st.featureEng.WindowSize),
				EntryPrice: trade.EntryPrice, ExitPrice: trade.ExitPrice,
			})

			st.position = model.Position{Side: "flat"}
			st.highestPrice = 0
			st.cooldownBars = 50 // 卖出后冷却50个bar
			return trade
		}
	}

	// 正常买入
	if signal.Action == "buy" && st.position.Side == "flat" && signal.Volume > 0 {
		execPrice := price * (1 + st.slippage)
		cost := float64(signal.Volume) * execPrice * (1 + st.commission)
		if cost > st.cash {
			return nil
		}
		st.cash -= cost
		st.position = model.Position{
			Symbol: signal.Symbol, Side: "long", EntryTime: bar.EndTime,
			EntryPrice: execPrice, Volume: signal.Volume, AvgCost: execPrice,
		}
		st.highestPrice = execPrice
		st.dayBuyCount++ // 递增日买入计数
		log.Printf("[trade] BUY: vol=%d price=%.2f cost=%.2f cash=%.2f stopLoss=%.2f target=%.2f",
			signal.Volume, execPrice, cost, st.cash, signal.StopLossPrice, signal.TargetPrice)
		return nil
	}

	// 正常卖出（需更强理由）
	if signal.Action == "sell" && st.position.Side == "long" && st.position.HoldBars > 10 {
		unrealPct := 0.0
		if st.position.AvgCost > 0 {
			unrealPct = (price - st.position.AvgCost) / st.position.AvgCost * 100
		}

		shouldSell := false
		// 卖出信号confidence在0-0.5范围（direction=-1时）
		if unrealPct < -0.5 && signal.Confidence < 0.40 {
			shouldSell = true
		}
		if unrealPct > 0.5 && signal.Confidence < 0.45 {
			shouldSell = true
		}
		if unrealPct < -1.5 {
			shouldSell = true
		}

		if !shouldSell {
			return nil
		}

		execPrice := price * (1 - st.slippage)
		proceeds := float64(st.position.Volume) * execPrice * (1 - st.commission)
		st.cash += proceeds
		pnl := (execPrice - st.position.AvgCost) * float64(st.position.Volume)
		pnlPct := (execPrice - st.position.AvgCost) / st.position.AvgCost * 100

		trade := &model.TradeRecord{
			EntryTime: st.position.EntryTime, ExitTime: bar.EndTime, Side: "buy",
			EntryPrice: st.position.AvgCost, ExitPrice: execPrice,
			Volume: st.position.Volume, PnL: pnl, PnLPct: pnlPct, HoldBars: st.position.HoldBars,
		}
		st.totalTrades++
		if pnl > 0 {
			st.winningTrades++
		}
		st.position = model.Position{Side: "flat"}
		st.highestPrice = 0
		st.cooldownBars = 50 // 卖出后冷却50个bar

		collector.RecordTrade(&monitor.TradeRecord{
			Timestamp: fmt.Sprintf("%s %s", signal.Date, bar.EndTime), Symbol: signal.Symbol,
			Side: "sell", Price: execPrice, Volume: trade.Volume, PnL: pnl, PnLPct: pnlPct,
			HoldTime:   float64(trade.HoldBars) * float64(st.featureEng.WindowSize),
			EntryPrice: trade.EntryPrice, ExitPrice: trade.ExitPrice,
		})
		return trade
	}

	return nil
}

// forceClose 强制平仓
func (st *SimTrader) forceClose(price float64, timeStr string) *model.TradeRecord {
	if st.position.Side != "long" {
		return nil
	}
	execPrice := price * (1 - st.slippage)
	proceeds := float64(st.position.Volume) * execPrice * (1 - st.commission)
	st.cash += proceeds
	pnl := (execPrice - st.position.AvgCost) * float64(st.position.Volume)
	pnlPct := (execPrice - st.position.AvgCost) / st.position.AvgCost * 100
	trade := &model.TradeRecord{
		EntryTime: st.position.EntryTime, ExitTime: timeStr, Side: "buy",
		EntryPrice: st.position.AvgCost, ExitPrice: execPrice,
		Volume: st.position.Volume, PnL: pnl, PnLPct: pnlPct, HoldBars: st.position.HoldBars,
	}
	st.totalTrades++
	if pnl > 0 {
		st.winningTrades++
	}
	st.position = model.Position{Side: "flat"}
	st.highestPrice = 0
	st.cooldownBars = 50 // 强制平仓后冷却50个bar
	return trade
}

// GetStats 获取统计
func (st *SimTrader) GetStats() (cash float64, totalTrades, winningTrades, totalSignals, buySignals, sellSignals, holdSignals int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.cash, st.totalTrades, st.winningTrades, st.totalSignals, st.buySignals, st.sellSignals, st.holdSignals
}

// ========== 技术指标函数 ==========

// calcRSI 计算RSI（Wilder平滑法，防极端值）
func (st *SimTrader) calcRSI(bars []model.TickBar, period int) float64 {
	if len(bars) < period+1 {
		return 50.0
	}
	gains := make([]float64, 0, len(bars)-1)
	losses := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		change := bars[i].Close - bars[i-1].Close
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}
	if len(gains) < period {
		return 50.0
	}
	avgGain := 0.0
	avgLoss := 0.0
	for i := 0; i < period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := period; i < len(gains); i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
	}
	if avgLoss < 1e-10 {
		if avgGain > 1e-10 {
			return 95.0
		}
		return 50.0
	}
	rs := avgGain / avgLoss
	rsi := 100.0 - 100.0/(1.0+rs)
	if rsi < 5 {
		rsi = 5
	}
	if rsi > 95 {
		rsi = 95
	}
	return rsi
}

// calcEMA 计算指数移动平均
func (st *SimTrader) calcEMA(bars []model.TickBar, period int) float64 {
	if len(bars) < period {
		return bars[len(bars)-1].Close
	}
	k := 2.0 / (float64(period) + 1)
	ema := bars[0].Close
	for i := 1; i < len(bars); i++ {
		ema = bars[i].Close*k + ema*(1-k)
	}
	return ema
}

// calcMACD 计算MACD
func (st *SimTrader) calcMACD(bars []model.TickBar) (macdLine, signalLine, histogram float64) {
	if len(bars) < 35 {
		return 0, 0, 0
	}
	ema12 := bars[0].Close
	ema26 := bars[0].Close
	k12 := 2.0 / 13.0
	k26 := 2.0 / 27.0
	macdValues := make([]float64, 0)
	for i := 1; i < len(bars); i++ {
		ema12 = bars[i].Close*k12 + ema12*(1-k12)
		ema26 = bars[i].Close*k26 + ema26*(1-k26)
		macdValues = append(macdValues, ema12-ema26)
	}
	if len(macdValues) < 9 {
		return macdValues[len(macdValues)-1], 0, macdValues[len(macdValues)-1]
	}
	signalEma := macdValues[0]
	ks := 2.0 / 10.0
	for i := 1; i < len(macdValues); i++ {
		signalEma = macdValues[i]*ks + signalEma*(1-ks)
	}
	macdLine = macdValues[len(macdValues)-1]
	signalLine = signalEma
	histogram = macdLine - signalLine
	return
}

// calcBollinger 计算布林带
func (st *SimTrader) calcBollinger(bars []model.TickBar, period int, mult float64) (upper, middle, lower float64) {
	if len(bars) < period {
		p := bars[len(bars)-1].Close
		return p, p, p
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Close
	}
	middle = sum / float64(period)
	variance := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		diff := bars[i].Close - middle
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))
	upper = middle + mult*stdDev
	lower = middle - mult*stdDev
	return
}

// trueRange 计算真实波幅
func (st *SimTrader) trueRange(bars []model.TickBar, i int) float64 {
	if i == 0 {
		return bars[i].High - bars[i].Low
	}
	tr1 := bars[i].High - bars[i].Low
	tr2 := math.Abs(bars[i].High - bars[i-1].Close)
	tr3 := math.Abs(bars[i].Low - bars[i-1].Close)
	return math.Max(tr1, math.Max(tr2, tr3))
}

// calcATR 计算平均真实波幅
func (st *SimTrader) calcATR(bars []model.TickBar, period int) float64 {
	if len(bars) < period+1 {
		if len(bars) > 0 {
			return bars[len(bars)-1].High - bars[len(bars)-1].Low
		}
		return 0.01
	}
	atr := 0.0
	for i := 1; i <= period; i++ {
		atr += st.trueRange(bars, i)
	}
	atr /= float64(period)
	for i := period + 1; i < len(bars); i++ {
		tr := st.trueRange(bars, i)
		atr = (atr*float64(period-1) + tr) / float64(period)
	}
	if atr < 0.01 {
		atr = 0.01
	}
	return atr
}

// ========== 配置与数据加载 ==========

// loadAllDayTicks 递归扫描年份/日期目录加载指定标的 tick 数据
// 目录结构: tickDir/2019/2019-01-02/002484.csv
func loadAllDayTicks(tickDir, symbol string) ([]model.DayTicks, error) {
	var allDays []model.DayTicks

	yearEntries, err := os.ReadDir(tickDir)
	if err != nil {
		return nil, fmt.Errorf("read tick dir %s: %w", tickDir, err)
	}

	for _, yearEntry := range yearEntries {
		yearPath := filepath.Join(tickDir, yearEntry.Name())
		if !yearEntry.IsDir() {
			continue
		}

		// 扫描年份目录下的日期目录
		dayEntries, err := os.ReadDir(yearPath)
		if err != nil {
			log.Printf("skip year dir %s: %v", yearPath, err)
			continue
		}

		for _, dayEntry := range dayEntries {
			dayPath := filepath.Join(yearPath, dayEntry.Name())
			if !dayEntry.IsDir() {
				continue
			}
			// 日期目录，使用 LoadMultiDayDir 加载指定标的
			days, err := loader.LoadMultiDayDir(dayPath, symbol, dayEntry.Name())
			if err != nil {
				log.Printf("skip day dir %s: %v", dayPath, err)
				continue
			}
			allDays = append(allDays, days...)
		}
	}

	sort.Slice(allDays, func(i, j int) bool {
		return allDays[i].Date < allDays[j].Date
	})
	return allDays, nil
}

// ========== 应用主体 ==========

type App struct {
	config    *config.Config
	collector *monitor.MonitorCollector
	trader    *SimTrader
	allDays   []model.DayTicks
	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
}

type TradeCommand struct {
	Action string `json:"action"`
	Speed  int    `json:"speed"`
	Year   string `json:"year"`
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	collector := monitor.NewMonitorCollector()

	app := &App{
		config:    cfg,
		collector: collector,
		stopCh:    make(chan struct{}),
	}

	log.Printf("loading tick data from %s ...", cfg.Data.TickDir)
	days, err := loadAllDayTicks(cfg.Data.TickDir, cfg.Pipeline.Symbol)
	if err != nil {
		log.Fatalf("load tick data: %v", err)
	}
	app.allDays = days
	log.Printf("loaded %d trading days", len(days))

	// HTTP 路由
	http.HandleFunc("/api/dashboard", app.handleDashboard)
	http.HandleFunc("/api/trading", app.handleTrading)
	http.HandleFunc("/api/training", app.handleTraining)
	http.HandleFunc("/api/returns", app.handleReturns)
	http.HandleFunc("/api/risk", app.handleRisk)
	http.HandleFunc("/api/trade", app.handleTradeCmd)
	http.HandleFunc("/api/start", app.handleStart)
	http.HandleFunc("/api/stop", app.handleStop)
	http.HandleFunc("/api/reset", app.handleReset)
	http.HandleFunc("/api/config", app.handleGetConfig)
	http.HandleFunc("/api/health", app.handleHealth)
	http.HandleFunc("/", app.handleIndex)

	go func() {
		log.Printf("API server starting on :%s", cfg.Server.Port)
		if err := http.ListenAndServe(":"+cfg.Server.Port, nil); err != nil {
			log.Fatalf("API server error: %v", err)
		}
	}()

	commandLoop(app)
}

func commandLoop(app *App) {
	log.Println("=== Monitor Ready ===")
	log.Println("Commands: start | stop | reset | status | exit")
	log.Printf("Web UI: http://localhost:%s", app.config.Server.Port)

	for {
		var cmd string
		fmt.Print("> ")
		fmt.Scanln(&cmd)

		switch cmd {
		case "start":
			app.runSimulation()
		case "stop":
			app.stopSimulation()
		case "reset":
			app.resetSimulation()
		case "status":
			app.printStatus()
		case "exit":
			app.stopSimulation()
			log.Println("bye")
			return
		default:
			log.Printf("unknown command: %s", cmd)
		}
	}
}

func (app *App) runSimulation() {
	app.mu.Lock()
	if app.running {
		app.mu.Unlock()
		log.Println("simulation already running")
		return
	}
	app.running = true
	app.trader = NewSimTrader(app.config)
	app.mu.Unlock()

	app.collector.UpdateTradeControl(&monitor.TradeControl{
		Mode:        "simulation",
		Status:      "running",
		InitialCash: app.config.Backtest.InitialCash,
		Symbol:      app.config.Pipeline.Symbol,
		ReplaySpeed: 500,
		StartTime:   time.Now().Format("2006-01-02 15:04:05"),
	})

	go app.executeSimulation()
}

func (app *App) executeSimulation() {
	defer func() {
		app.mu.Lock()
		app.running = false
		app.mu.Unlock()
		app.collector.UpdateTradeControl(&monitor.TradeControl{
			Mode:   "simulation",
			Status: "stopped",
		})
	}()

	totalDays := len(app.allDays)
	trainDays := int(float64(totalDays) * app.config.Pipeline.TrainRatio)

	log.Printf("simulation: %d total days, %d train, %d infer", totalDays, trainDays, totalDays-trainDays)

	// 积累阶段
	for i := 0; i < trainDays && i < totalDays; i++ {
		day := app.allDays[i]
		bars := app.trader.featureEng.AggregateBars(day.Ticks)
		if len(bars) > 0 {
			app.trader.bars = append(app.trader.bars, bars...)
		}
		app.collector.UpdateTradeControl(&monitor.TradeControl{
			CurrentDay:  day.Date,
			TotalDays:   totalDays - trainDays,
			ProgressPct: float64(i) / float64(totalDays) * 100,
		})
	}
	log.Printf("accumulation done: %d bars collected", len(app.trader.bars))

	// 推理阶段
	for i := trainDays; i < totalDays; i++ {
		app.mu.Lock()
		if !app.running {
			app.mu.Unlock()
			return
		}
		app.mu.Unlock()

		day := app.allDays[i]
		app.trader.ProcessDay(day.Date, day.Symbol, day.Ticks, app.collector)

		progress := float64(i-trainDays+1) / float64(totalDays-trainDays) * 100
		app.collector.UpdateTradeControl(&monitor.TradeControl{
			CurrentDay:  day.Date,
			TotalDays:   totalDays - trainDays,
			ProgressPct: progress,
		})

		if i%10 == 0 || i == totalDays-1 {
			cash, totalTr, winTr, _, buySig, sellSig, holdSig := app.trader.GetStats()
			winRate := 0.0
			if totalTr > 0 {
				winRate = float64(winTr) / float64(totalTr) * 100
			}
			totalValue := cash
			if app.trader.position.Side == "long" {
				lastBar := app.trader.bars[len(app.trader.bars)-1]
				totalValue += float64(app.trader.position.Volume) * lastBar.Close
			}
			returnPct := (totalValue - app.config.Backtest.InitialCash) / app.config.Backtest.InitialCash * 100
			log.Printf("[sim] day=%s progress=%.0f%% cash=%.2f totalValue=%.2f return=%.2f%% trades=%d winRate=%.1f%% signals=%d/%d/%d",
				day.Date, progress, cash, totalValue, returnPct, totalTr, winRate, buySig, sellSig, holdSig)
		}
	}

	// 最终统计
	cash, totalTr, winTr, totalSig, buySig, sellSig, holdSig := app.trader.GetStats()
	winRate := 0.0
	if totalTr > 0 {
		winRate = float64(winTr) / float64(totalTr) * 100
	}
	totalValue := cash
	returnPct := (totalValue - app.config.Backtest.InitialCash) / app.config.Backtest.InitialCash * 100
	log.Printf("=== SIMULATION COMPLETE ===")
	log.Printf("final cash=%.2f totalValue=%.2f return=%.2f%%", cash, totalValue, returnPct)
	log.Printf("trades=%d winRate=%.1f%% signals=buy:%d/sell:%d/hold:%d/total:%d",
		totalTr, winRate, buySig, sellSig, holdSig, totalSig)
}

func (app *App) stopSimulation() {
	app.mu.Lock()
	app.running = false
	app.mu.Unlock()
	log.Println("simulation stopped")
}

func (app *App) resetSimulation() {
	app.stopSimulation()
	app.trader = nil
	app.collector.ResetData(app.config.Backtest.InitialCash)
	log.Println("simulation reset")
}

func (app *App) printStatus() {
	app.mu.Lock()
	running := app.running
	app.mu.Unlock()
	ctrl := app.collector.GetTradeControl()
	cash, totalTr, _, _, _, _, _ := 0.0, 0, 0, 0, 0, 0, 0
	if app.trader != nil {
		cash, totalTr, _, _, _, _, _ = app.trader.GetStats()
	}
	log.Printf("running=%v mode=%s day=%s progress=%.1f%% cash=%.2f trades=%d",
		running, ctrl.Mode, ctrl.CurrentDay, ctrl.ProgressPct, cash, totalTr)
}

// ========== HTTP Handlers ==========

func (app *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data := app.collector.GetDashboardData()
	app.jsonResponse(w, data)
}

func (app *App) handleTrading(w http.ResponseWriter, r *http.Request) {
	ctrl := app.collector.GetTradeControl()
	app.jsonResponse(w, ctrl)
}

func (app *App) handleTraining(w http.ResponseWriter, r *http.Request) {
	app.jsonResponse(w, map[string]interface{}{
		"status":  "idle",
		"message": "multiFactorPredict uses built-in algorithm",
	})
}

func (app *App) handleReturns(w http.ResponseWriter, r *http.Request) {
	cash := 0.0
	if app.trader != nil {
		cash, _, _, _, _, _, _ = app.trader.GetStats()
	}
	returnPct := 0.0
	if app.config.Backtest.InitialCash > 0 {
		returnPct = (cash - app.config.Backtest.InitialCash) / app.config.Backtest.InitialCash * 100
	}
	app.jsonResponse(w, map[string]interface{}{
		"initial_cash":  app.config.Backtest.InitialCash,
		"current_value": cash,
		"total_return":  returnPct,
		"cash":          cash,
	})
}

func (app *App) handleRisk(w http.ResponseWriter, r *http.Request) {
	app.jsonResponse(w, map[string]interface{}{"status": "ok"})
}

func (app *App) handleTradeCmd(w http.ResponseWriter, r *http.Request) {
	var cmd TradeCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch cmd.Action {
	case "start":
		app.runSimulation()
	case "stop":
		app.stopSimulation()
	case "reset":
		app.resetSimulation()
	}
	app.jsonResponse(w, map[string]string{"status": "ok"})
}

func (app *App) handleStart(w http.ResponseWriter, r *http.Request) {
	app.runSimulation()
	app.jsonResponse(w, map[string]string{"status": "started"})
}

func (app *App) handleStop(w http.ResponseWriter, r *http.Request) {
	app.stopSimulation()
	app.jsonResponse(w, map[string]string{"status": "stopped"})
}

func (app *App) handleReset(w http.ResponseWriter, r *http.Request) {
	app.resetSimulation()
	app.jsonResponse(w, map[string]string{"status": "reset"})
}

func (app *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	app.jsonResponse(w, app.config)
}

func (app *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	app.jsonResponse(w, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	webPath := filepath.Join("internal", "monitor", "web", "index.html")
	data, err := os.ReadFile(webPath)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Monitor</title></head><body>
<h1>Trading Monitor</h1><p>API: <a href="/api/dashboard">/api/dashboard</a></p>
</body></html>`)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (app *App) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(data)
}
