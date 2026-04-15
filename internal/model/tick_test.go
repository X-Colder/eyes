package model

import (
	"encoding/json"
	"testing"
)

func TestTrendPhaseString(t *testing.T) {
	tests := []struct {
		phase TrendPhase
		want  string
	}{
		{TrendUnknown, "unknown"},
		{TrendRising, "rising"},
		{TrendPeak, "peak"},
		{TrendFalling, "falling"},
		{TrendTrough, "trough"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("TrendPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestTickDataJSON(t *testing.T) {
	tick := TickData{
		TranID: 172291, Time: "09:25:00", Price: 7.84,
		Volume: 1900, SaleOrderVolume: 2500, BuyOrderVolume: 1900,
		Type: "S", SaleOrderID: 162351, SaleOrderPrice: 7.84,
		BuyOrderID: 86382, BuyOrderPrice: 7.84,
	}
	data, err := json.Marshal(tick)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TickData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.TranID != tick.TranID || got.Price != tick.Price || got.Type != tick.Type {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestTradeSignalJSON(t *testing.T) {
	sig := TradeSignal{
		Date: "2018-05-18", Time: "09:30:00", Symbol: "002484",
		Action: "buy", Confidence: 0.75, CurrentPrice: 7.85,
		TargetPrice: 7.95, StopLossPrice: 7.78, Volume: 1000,
		WinRate: 0.625, OddsRatio: 1.5, ProfitRate: 1.27,
		KellyFraction: 0.15, Phase: "rising",
	}
	data, err := json.Marshal(sig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TradeSignal
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Action != "buy" || got.Symbol != "002484" || got.KellyFraction != 0.15 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestPositionJSON(t *testing.T) {
	pos := Position{
		Symbol: "002484", Side: "long", EntryTime: "09:30:00",
		EntryPrice: 7.85, Volume: 1000, AvgCost: 7.85,
		UnrealPnL: 100.0, UnrealPct: 1.27, HoldBars: 5,
	}
	data, err := json.Marshal(pos)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Position
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Side != "long" || got.Volume != 1000 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestPipelineStateInit(t *testing.T) {
	state := PipelineState{
		Symbol: "002484", Phase: "idle", Cash: 100000, TotalValue: 100000,
	}
	if state.CumulativePnL != 0 {
		t.Errorf("expected zero PnL, got %.2f", state.CumulativePnL)
	}
	if len(state.DailyResults) != 0 {
		t.Errorf("expected empty daily results")
	}
}
