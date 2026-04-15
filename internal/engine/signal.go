package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"

	"github.com/eyes/internal/analysis"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/model"
)

// SignalEngine 实时交易信号引擎
// tick 数据流入 → bar 聚合 → 特征提取 → 模型预测 → 信号生成（含仓位计算）
type SignalEngine struct {
	eng        *feature.Engineer
	ta         *analysis.TrendAnalyzer
	serviceURL string // Python 推理服务地址

	// 运行时状态
	bars     []model.TickBar // 已聚合的 bar 历史
	position model.Position  // 当前持仓
	cash     float64         // 可用资金
	signals  []model.TradeSignal
	trades   []model.TradeRecord

	// 配置
	Commission  float64
	Slippage    float64
	MaxPosition int64
	BarInterval int
}

// NewSignalEngine 创建信号引擎
func NewSignalEngine(eng *feature.Engineer, serviceURL string, initialCash float64, commission, slippage float64, maxPosition int64) *SignalEngine {
	return &SignalEngine{
		eng:         eng,
		ta:          analysis.NewTrendAnalyzer(5, 0.02),
		serviceURL:  serviceURL,
		cash:        initialCash,
		Commission:  commission,
		Slippage:    slippage,
		MaxPosition: maxPosition,
		BarInterval: eng.BarInterval,
	}
}

// ProcessDayTicks 处理一整天的 tick 数据，逐 bar 生成信号
// 模拟实时场景：每产生一根新 bar 就做一次预测
func (se *SignalEngine) ProcessDayTicks(date, symbol string, ticks []model.TickData) ([]model.TradeSignal, []model.TradeRecord) {
	bars := se.eng.AggregateBars(ticks)
	if len(bars) == 0 {
		return nil, nil
	}
	log.Printf("[signal] processing %s: %d ticks -> %d bars", date, len(ticks), len(bars))

	var daySignals []model.TradeSignal
	var dayTrades []model.TradeRecord
	windowSize := se.eng.WindowSize

	for i, bar := range bars {
		se.bars = append(se.bars, bar)

		// 需要至少 windowSize 根 bar 才能提取特征
		if len(se.bars) < windowSize {
			continue
		}

		// 取最近 windowSize 根 bar 提取特征
		window := se.bars[len(se.bars)-windowSize:]
		feats := se.eng.ExtractFeaturesWithMeta(
			se.bars[max(0, len(se.bars)-windowSize-1):], date, symbol,
		)
		if len(feats) == 0 {
			continue
		}
		feat := feats[len(feats)-1]

		// 趋势阶段
		phases := se.ta.IdentifyPhases(se.bars)
		currentPhase := model.TrendUnknown
		if len(phases) > 0 {
			currentPhase = phases[len(phases)-1]
		}

		// 调用推理服务
		pred, err := se.callPredict(feat)
		if err != nil {
			log.Printf("[signal] predict error at %s: %v", bar.EndTime, err)
			continue
		}

		// 生成交易信号
		signal := se.generateSignal(date, bar, pred, currentPhase, window)
		daySignals = append(daySignals, signal)
		se.signals = append(se.signals, signal)

		// 执行交易逻辑
		trade := se.executeTrade(signal, bar, i, len(bars))
		if trade != nil {
			dayTrades = append(dayTrades, *trade)
			se.trades = append(se.trades, *trade)
		}
	}

	return daySignals, dayTrades
}

// generateSignal 根据预测结果生成完整交易信号
func (se *SignalEngine) generateSignal(date string, bar model.TickBar, pred model.PredictionResult, phase model.TrendPhase, window []model.TickBar) model.TradeSignal {
	price := bar.Close

	// 计算历史胜率（基于最近 N 个信号的准确性）
	winRate := se.estimateWinRate(pred)

	// 计算赔率 = 平均盈利 / 平均亏损
	avgWin, avgLoss := se.estimateAvgWinLoss(window)
	oddsRatio := 1.0
	if avgLoss > 0 {
		oddsRatio = avgWin / avgLoss
	}

	// Kelly 公式: f* = (p * b - q) / b
	// p=胜率, q=1-p, b=赔率
	kellyFrac := 0.0
	if oddsRatio > 0 {
		kellyFrac = (winRate*oddsRatio - (1 - winRate)) / oddsRatio
	}
	kellyFrac = math.Max(0, math.Min(kellyFrac, 0.25)) // 限制最大 25%

	// 计算建议交易量
	volume := int64(0)
	if pred.Action == "buy" && se.position.Side == "flat" {
		positionValue := se.cash * kellyFrac
		volume = int64(positionValue / (price * (1 + se.Commission + se.Slippage)))
		if volume > se.MaxPosition {
			volume = se.MaxPosition
		}
	} else if pred.Action == "sell" && se.position.Side == "long" {
		volume = se.position.Volume
	}

	// 目标价 & 止损价（基于波动率）
	volatility := se.calcVolatility(window)
	targetPrice := price * (1 + volatility*2)
	stopLossPrice := price * (1 - volatility)
	profitRate := (targetPrice - price) / price * 100

	// 预估持仓时间
	holdBars := 5 // 默认 5 根 bar
	if pred.Confidence > 0.7 {
		holdBars = 10
	}
	holdSeconds := holdBars * se.BarInterval

	return model.TradeSignal{
		Date:           date,
		Time:           bar.EndTime,
		Symbol:         pred.Symbol,
		Action:         pred.Action,
		RiseProb:       pred.RiseProb,
		FallProb:       pred.FallProb,
		Confidence:     pred.Confidence,
		CurrentPrice:   price,
		TargetPrice:    targetPrice,
		StopLossPrice:  stopLossPrice,
		Volume:         volume,
		WinRate:        winRate,
		OddsRatio:      oddsRatio,
		ProfitRate:     profitRate,
		ExpectedProfit: float64(volume) * price * profitRate / 100,
		HoldBars:       holdBars,
		HoldSeconds:    holdSeconds,
		ExpectSellTime: fmt.Sprintf("+%ds", holdSeconds),
		KellyFraction:  kellyFrac,
		Phase:          phase.String(),
	}
}

// executeTrade 根据信号执行交易
func (se *SignalEngine) executeTrade(signal model.TradeSignal, bar model.TickBar, barIdx, totalBars int) *model.TradeRecord {
	price := bar.Close

	if signal.Action == "buy" && se.position.Side == "flat" && signal.Volume > 0 && signal.Confidence > 0.6 {
		execPrice := price * (1 + se.Slippage)
		cost := float64(signal.Volume) * execPrice * (1 + se.Commission)
		if cost > se.cash {
			return nil
		}
		se.cash -= cost
		se.position = model.Position{
			Symbol: signal.Symbol, Side: "long",
			EntryTime: bar.EndTime, EntryPrice: execPrice,
			Volume: signal.Volume, AvgCost: execPrice,
		}
		return nil // 开仓不算完成交易
	}

	if signal.Action == "sell" && se.position.Side == "long" {
		execPrice := price * (1 - se.Slippage)
		proceeds := float64(se.position.Volume) * execPrice * (1 - se.Commission)
		se.cash += proceeds
		pnl := (execPrice - se.position.AvgCost) * float64(se.position.Volume)
		pnlPct := (execPrice - se.position.AvgCost) / se.position.AvgCost * 100

		trade := &model.TradeRecord{
			EntryTime:  se.position.EntryTime,
			ExitTime:   bar.EndTime,
			Side:       "buy",
			EntryPrice: se.position.AvgCost,
			ExitPrice:  execPrice,
			Volume:     se.position.Volume,
			PnL:        pnl,
			PnLPct:     pnlPct,
			HoldBars:   se.position.HoldBars,
		}
		se.position = model.Position{Side: "flat"}
		return trade
	}

	// 更新持仓 bar 计数
	if se.position.Side == "long" {
		se.position.HoldBars++
		se.position.UnrealPnL = (price - se.position.AvgCost) * float64(se.position.Volume)
		if se.position.AvgCost > 0 {
			se.position.UnrealPct = (price - se.position.AvgCost) / se.position.AvgCost * 100
		}
	}
	return nil
}

// callPredict 调用 Python 推理服务
func (se *SignalEngine) callPredict(feat model.Feature) (model.PredictionResult, error) {
	body := map[string]interface{}{
		"features":    [][]float64{feat.Values},
		"window_size": se.eng.WindowSize,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(se.serviceURL+"/predict", "application/json", bytes.NewReader(data))
	if err != nil {
		return model.PredictionResult{}, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)

	var preds []model.PredictionResult
	if err := json.Unmarshal(respData, &preds); err != nil {
		return model.PredictionResult{}, fmt.Errorf("decode: %w", err)
	}
	if len(preds) == 0 {
		return model.PredictionResult{}, fmt.Errorf("empty prediction")
	}
	preds[0].Time = feat.Time
	preds[0].Symbol = feat.Symbol
	return preds[0], nil
}

// --- 内部工具 ---

func (se *SignalEngine) estimateWinRate(pred model.PredictionResult) float64 {
	// 置信度越高胜率越高，基础胜率 50%
	return 0.5 + (pred.Confidence-0.5)*0.5
}

func (se *SignalEngine) estimateAvgWinLoss(window []model.TickBar) (avgWin, avgLoss float64) {
	if len(window) < 2 {
		return 0.02, 0.01
	}
	var wins, losses []float64
	for i := 1; i < len(window); i++ {
		chg := (window[i].Close - window[i-1].Close) / window[i-1].Close
		if chg > 0 {
			wins = append(wins, chg)
		} else {
			losses = append(losses, math.Abs(chg))
		}
	}
	if len(wins) > 0 {
		for _, w := range wins {
			avgWin += w
		}
		avgWin /= float64(len(wins))
	}
	if len(losses) > 0 {
		for _, l := range losses {
			avgLoss += l
		}
		avgLoss /= float64(len(losses))
	}
	if avgLoss == 0 {
		avgLoss = 0.001
	}
	return
}

func (se *SignalEngine) calcVolatility(window []model.TickBar) float64 {
	if len(window) < 2 {
		return 0.01
	}
	var returns []float64
	for i := 1; i < len(window); i++ {
		if window[i-1].Close > 0 {
			returns = append(returns, (window[i].Close-window[i-1].Close)/window[i-1].Close)
		}
	}
	if len(returns) == 0 {
		return 0.01
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	variance := 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	return math.Sqrt(variance / float64(len(returns)))
}

// GetState 获取引擎状态
func (se *SignalEngine) GetState() (model.Position, float64, []model.TradeRecord) {
	return se.position, se.cash, se.trades
}

// ForceClose 强制平仓（日终或需要时调用）
func (se *SignalEngine) ForceClose(price float64, timeStr string) *model.TradeRecord {
	if se.position.Side != "long" {
		return nil
	}
	execPrice := price * (1 - se.Slippage)
	proceeds := float64(se.position.Volume) * execPrice * (1 - se.Commission)
	se.cash += proceeds
	pnl := (execPrice - se.position.AvgCost) * float64(se.position.Volume)
	pnlPct := (execPrice - se.position.AvgCost) / se.position.AvgCost * 100
	trade := &model.TradeRecord{
		EntryTime: se.position.EntryTime, ExitTime: timeStr, Side: "buy",
		EntryPrice: se.position.AvgCost, ExitPrice: execPrice,
		Volume: se.position.Volume, PnL: pnl, PnLPct: pnlPct, HoldBars: se.position.HoldBars,
	}
	se.trades = append(se.trades, *trade)
	se.position = model.Position{Side: "flat"}
	return trade
}
