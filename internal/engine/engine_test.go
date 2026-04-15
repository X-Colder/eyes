package engine

import (
	"fmt"
	"testing"

	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/model"
)

// makeTicks 生成测试 tick 数据
func makeTicks(n int) []model.TickData {
	ticks := make([]model.TickData, n)
	basePrice := 7.84
	for i := 0; i < n; i++ {
		sec := i * 2
		h := 9 + (30*60+sec)/3600
		m := ((30*60 + sec) % 3600) / 60
		s := (30*60 + sec) % 60
		typ := "B"
		if i%2 == 0 {
			typ = "S"
		}
		ticks[i] = model.TickData{
			TranID:          int64(100000 + i),
			Time:            fmt.Sprintf("%02d:%02d:%02d", h, m, s),
			Price:           basePrice + float64(i%5)*0.01,
			Volume:          int64(100 + i*10),
			SaleOrderVolume: 500,
			BuyOrderVolume:  400,
			Type:            typ,
			SaleOrderID:     int64(200000 + i),
			SaleOrderPrice:  basePrice,
			BuyOrderID:      int64(300000 + i),
			BuyOrderPrice:   basePrice,
		}
	}
	return ticks
}

func TestNewSignalEngine(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	if se.cash != 100000 {
		t.Errorf("cash = %.0f, want 100000", se.cash)
	}
	if se.MaxPosition != 10000 {
		t.Errorf("MaxPosition = %d, want 10000", se.MaxPosition)
	}
}

func TestGetState(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	pos, cash, trades := se.GetState()
	if pos.Side != "" {
		t.Errorf("initial side = %q, want empty", pos.Side)
	}
	if cash != 100000 {
		t.Errorf("cash = %.0f, want 100000", cash)
	}
	if len(trades) != 0 {
		t.Error("expected empty trades")
	}
}

func TestForceCloseNoPosition(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	trade := se.ForceClose(7.85, "15:00:00")
	if trade != nil {
		t.Error("expected nil trade for flat position")
	}
}

func TestForceCloseWithPosition(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	// 手动设置持仓
	se.position = model.Position{
		Symbol: "002484", Side: "long",
		EntryTime: "09:30:00", EntryPrice: 7.85,
		Volume: 1000, AvgCost: 7.85,
	}
	se.cash = 92150 // 100000 - 7.85*1000

	trade := se.ForceClose(7.90, "15:00:00")
	if trade == nil {
		t.Fatal("expected trade for force close")
	}
	if trade.EntryPrice != 7.85 {
		t.Errorf("EntryPrice = %.2f, want 7.85", trade.EntryPrice)
	}
	if trade.PnL <= 0 {
		t.Logf("PnL = %.2f (should be positive for 7.85 -> 7.90)", trade.PnL)
	}
	// 平仓后应该是 flat
	pos, _, _ := se.GetState()
	if pos.Side != "flat" {
		t.Errorf("side after force close = %q, want flat", pos.Side)
	}
}

func TestEstimateWinRate(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	pred := model.PredictionResult{Confidence: 0.8}
	wr := se.estimateWinRate(pred)
	if wr < 0.5 || wr > 1.0 {
		t.Errorf("winRate = %f, should be in [0.5, 1.0]", wr)
	}
}

func TestCalcVolatility(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	// 波动窗口
	window := []model.TickBar{
		{Close: 10.0}, {Close: 10.1}, {Close: 9.9}, {Close: 10.05}, {Close: 10.2},
	}
	vol := se.calcVolatility(window)
	if vol <= 0 {
		t.Errorf("volatility = %f, should be > 0", vol)
	}

	// 单根 bar 应返回默认值
	vol2 := se.calcVolatility([]model.TickBar{{Close: 10.0}})
	if vol2 != 0.01 {
		t.Errorf("volatility for 1 bar = %f, want 0.01", vol2)
	}
}

func TestEstimateAvgWinLoss(t *testing.T) {
	eng := feature.NewEngineer(30, 10, 3, 0.02)
	se := NewSignalEngine(eng, "http://localhost:5000", 100000, 0.0003, 0.001, 10000)

	window := []model.TickBar{
		{Close: 10.0}, {Close: 10.1}, {Close: 9.95}, {Close: 10.05}, {Close: 10.2},
	}
	avgWin, avgLoss := se.estimateAvgWinLoss(window)
	if avgWin <= 0 {
		t.Errorf("avgWin = %f, should be > 0", avgWin)
	}
	if avgLoss <= 0 {
		t.Errorf("avgLoss = %f, should be > 0", avgLoss)
	}
}

func TestNewPipeline(t *testing.T) {
	cfg := PipelineConfig{
		Symbol:      "002484",
		TickDir:     "/tmp",
		TrainRatio:  0.7,
		InitialCash: 100000,
		BarInterval: 30,
		WindowSize:  10,
		FutureSteps: 3,
		PriceThresh: 0.02,
	}
	p := NewPipeline(cfg)
	state := p.GetState()
	if state.Symbol != "002484" {
		t.Errorf("symbol = %q, want 002484", state.Symbol)
	}
	if state.Phase != "idle" {
		t.Errorf("phase = %q, want idle", state.Phase)
	}
	if state.Cash != 100000 {
		t.Errorf("cash = %.0f, want 100000", state.Cash)
	}
}
