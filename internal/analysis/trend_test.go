package analysis

import (
	"testing"

	"github.com/eyes/internal/model"
)

// makeBars 生成模拟 bar 数据（W 型走势：涨-跌-涨-跌）
func makeBars(n int) []model.TickBar {
	bars := make([]model.TickBar, n)
	q := n / 4
	for i := 0; i < n; i++ {
		var price float64
		switch {
		case i < q: // 上涨
			price = 10.0 + float64(i)*0.5
		case i < 2*q: // 下跌
			price = 10.0 + float64(q)*0.5 - float64(i-q)*0.5
		case i < 3*q: // 再上涨
			price = 10.0 + float64(i-2*q)*0.5
		default: // 再下跌
			price = 10.0 + float64(q)*0.5 - float64(i-3*q)*0.5
		}
		if price < 5.0 {
			price = 5.0
		}
		bars[i] = model.TickBar{
			StartTime: "09:30:00", EndTime: "09:30:30",
			Open: price - 0.02, High: price + 0.03,
			Low: price - 0.03, Close: price,
			Volume: 1000, Amount: price * 1000,
			TradeCount: 10, BuyVolume: 600, SellVolume: 400,
			VWAP: price,
		}
	}
	return bars
}

func TestIdentifyPhases(t *testing.T) {
	ta := NewTrendAnalyzer(5, 0.02)
	bars := makeBars(50)
	phases := ta.IdentifyPhases(bars)

	if len(phases) != len(bars) {
		t.Fatalf("phases len = %d, want %d", len(phases), len(bars))
	}

	// 检查至少有上涨和下跌阶段
	hasRising := false
	hasFalling := false
	for _, p := range phases {
		if p == model.TrendRising {
			hasRising = true
		}
		if p == model.TrendFalling {
			hasFalling = true
		}
	}
	if !hasRising {
		t.Error("expected at least some rising phases")
	}
	if !hasFalling {
		t.Error("expected at least some falling phases")
	}
}

func TestIdentifyPhasesEmpty(t *testing.T) {
	ta := NewTrendAnalyzer(5, 0.02)
	phases := ta.IdentifyPhases(nil)
	if phases != nil {
		t.Errorf("expected nil for empty bars")
	}
}

func TestLabelFeatures(t *testing.T) {
	ta := NewTrendAnalyzer(5, 0.02)
	bars := makeBars(50)

	// 创建简单特征（与 bar 时间对齐）
	feats := make([]model.Feature, len(bars))
	for i, bar := range bars {
		feats[i] = model.Feature{Time: bar.EndTime, Values: []float64{bar.Close}}
	}

	labeled := ta.LabelFeatures(feats, bars)
	if len(labeled) != len(feats) {
		t.Fatalf("labeled len = %d, want %d", len(labeled), len(feats))
	}
}

func TestGenerateTradeLabels(t *testing.T) {
	ta := NewTrendAnalyzer(5, 0.02)
	bars := makeBars(50)

	labels := ta.GenerateTradeLabels(bars)
	if len(labels) != len(bars) {
		t.Fatalf("labels len = %d, want %d", len(labels), len(bars))
	}

	hasBuy := false
	hasSell := false
	for _, l := range labels {
		if l.Action == 1 {
			hasBuy = true
		}
		if l.Action == 2 {
			hasSell = true
		}
		// Action 只能是 0/1/2
		if l.Action < 0 || l.Action > 2 {
			t.Errorf("invalid action %d", l.Action)
		}
	}
	// V 型走势应该有买入（低点）和卖出（高点）
	if !hasBuy {
		t.Error("expected buy labels in V-shape data")
	}
	if !hasSell {
		t.Error("expected sell labels in V-shape data")
	}
}

func TestNewTrendAnalyzer(t *testing.T) {
	ta := NewTrendAnalyzer(7, 0.03)
	if ta.SmoothWindow != 7 {
		t.Errorf("SmoothWindow = %d, want 7", ta.SmoothWindow)
	}
	if ta.PeakThresh != 0.03 {
		t.Errorf("PeakThresh = %f, want 0.03", ta.PeakThresh)
	}
}
