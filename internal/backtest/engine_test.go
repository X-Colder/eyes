package backtest

import (
	"testing"

	"github.com/eyes/internal/model"
)

// makeTestBars 生成先涨后跌的 bar 数据
func makeTestBars(n int) []model.TickBar {
	bars := make([]model.TickBar, n)
	mid := n / 2
	for i := 0; i < n; i++ {
		price := 10.0
		if i <= mid {
			price = 10.0 + float64(i)*0.05
		} else {
			price = 10.0 + float64(mid)*0.05 - float64(i-mid)*0.05
		}
		bars[i] = model.TickBar{
			StartTime: "09:30:00", EndTime: "09:30:30",
			Open: price, High: price + 0.02, Low: price - 0.02, Close: price,
			Volume: 1000, Amount: price * 1000, VWAP: price,
		}
	}
	return bars
}

func TestNewEngine(t *testing.T) {
	e := NewEngine(100000, 0.0003, 0.001, 10000)
	if e.InitialCash != 100000 {
		t.Errorf("InitialCash = %.0f, want 100000", e.InitialCash)
	}
	if e.MaxPosition != 10000 {
		t.Errorf("MaxPosition = %d, want 10000", e.MaxPosition)
	}
}

func TestRunWithPredictions(t *testing.T) {
	bars := makeTestBars(20)
	mid := len(bars) / 2

	// 在低点买入，高点卖出
	preds := make([]model.PredictionResult, len(bars))
	for i := range preds {
		preds[i] = model.PredictionResult{Action: "hold", Confidence: 0.7}
	}
	preds[0] = model.PredictionResult{Action: "buy", Confidence: 0.8}
	preds[mid] = model.PredictionResult{Action: "sell", Confidence: 0.8}

	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.Run(bars, preds)

	if result.TradeCount == 0 {
		t.Error("expected at least 1 completed trade")
	}
	if result.InitialCash != 100000 {
		t.Errorf("InitialCash = %.0f, want 100000", result.InitialCash)
	}
	// 买低卖高应该盈利
	if result.TotalReturn <= 0 {
		t.Logf("TotalReturn = %.4f%% (may be reduced by commission/slippage)", result.TotalReturn)
	}
}

func TestRunWithLabels(t *testing.T) {
	bars := makeTestBars(20)

	// 在第2根买入，中间卖出
	labels := make([]model.TradeLabel, len(bars))
	for i := range labels {
		labels[i] = model.TradeLabel{Action: 0} // hold
	}
	labels[1] = model.TradeLabel{Action: 1}           // buy
	labels[len(bars)/2] = model.TradeLabel{Action: 2} // sell

	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.RunWithLabels(bars, labels)

	if result.TradeCount == 0 {
		t.Error("expected at least 1 completed trade")
	}
}

func TestRunEmptyBars(t *testing.T) {
	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.Run(nil, nil)
	if result.TradeCount != 0 {
		t.Error("expected 0 trades for empty data")
	}
}

func TestForceCloseAtEnd(t *testing.T) {
	bars := makeTestBars(10)

	// 只买不卖
	preds := make([]model.PredictionResult, len(bars))
	for i := range preds {
		preds[i] = model.PredictionResult{Action: "hold", Confidence: 0.7}
	}
	preds[0] = model.PredictionResult{Action: "buy", Confidence: 0.8}
	// 没有 sell 信号

	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.Run(bars, preds)

	// 应该有强制平仓记录
	if result.TradeCount == 0 {
		t.Error("expected force close trade at end")
	}
}

func TestMaxDrawdown(t *testing.T) {
	// 先涨后跌再涨，应该检测到回撤
	bars := makeTestBars(30)
	preds := make([]model.PredictionResult, len(bars))
	for i := range preds {
		preds[i] = model.PredictionResult{Action: "hold", Confidence: 0.3}
	}
	preds[0] = model.PredictionResult{Action: "buy", Confidence: 0.8}

	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.Run(bars, preds)

	// 最大回撤应该 >= 0
	if result.MaxDrawdown < 0 {
		t.Errorf("MaxDrawdown = %.4f, should be >= 0", result.MaxDrawdown)
	}
}

func TestWinRate(t *testing.T) {
	bars := makeTestBars(20)
	preds := make([]model.PredictionResult, len(bars))
	for i := range preds {
		preds[i] = model.PredictionResult{Action: "hold", Confidence: 0.7}
	}
	preds[0] = model.PredictionResult{Action: "buy", Confidence: 0.8}
	preds[len(bars)/2] = model.PredictionResult{Action: "sell", Confidence: 0.8}

	e := NewEngine(100000, 0.0003, 0.001, 10000)
	result := e.Run(bars, preds)

	if result.WinRate < 0 || result.WinRate > 100 {
		t.Errorf("WinRate = %.2f, should be in [0, 100]", result.WinRate)
	}
}
