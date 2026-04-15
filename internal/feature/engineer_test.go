package feature

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyes/internal/model"
)

// makeTicks 生成测试 tick 数据（模拟一个交易日的多笔成交）
func makeTicks(n int) []model.TickData {
	ticks := make([]model.TickData, n)
	basePrice := 7.84
	for i := 0; i < n; i++ {
		sec := i * 2 // 每 2 秒一笔
		h := 9 + sec/3600
		m := (sec % 3600) / 60
		s := sec % 60
		typ := "B"
		if i%2 == 0 {
			typ = "S"
		}
		ticks[i] = model.TickData{
			TranID:          int64(100000 + i),
			Time:            timeFmt(h+30/60, m+30%60, s), // 从 09:30:00 开始
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

func timeFmt(h, m, s int) string {
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func TestAggregateBars(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	ticks := makeTicks(100)
	bars := eng.AggregateBars(ticks)

	if len(bars) == 0 {
		t.Fatal("expected bars > 0")
	}
	for _, bar := range bars {
		if bar.Open <= 0 || bar.Close <= 0 {
			t.Errorf("invalid bar prices: open=%.2f close=%.2f", bar.Open, bar.Close)
		}
		if bar.High < bar.Low {
			t.Errorf("high %.2f < low %.2f", bar.High, bar.Low)
		}
		if bar.Volume <= 0 {
			t.Errorf("volume should be > 0, got %d", bar.Volume)
		}
	}
}

func TestAggregateBarsEmpty(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	bars := eng.AggregateBars(nil)
	if len(bars) != 0 {
		t.Errorf("expected 0 bars for nil ticks, got %d", len(bars))
	}
}

func TestExtractFeatures(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	ticks := makeTicks(500)
	bars := eng.AggregateBars(ticks)

	feats := eng.ExtractFeatures(bars)
	if len(feats) == 0 {
		t.Fatal("expected features > 0")
	}

	// 特征维度：14 * windowSize + 10 = 14*10+10 = 150
	expectedDim := 14*eng.WindowSize + 10
	if len(feats[0].Values) != expectedDim {
		t.Errorf("feature dim = %d, want %d", len(feats[0].Values), expectedDim)
	}

	// 标签只能是 0 或 1
	for _, f := range feats {
		if f.Label != 0 && f.Label != 1 {
			t.Errorf("invalid label %d", f.Label)
		}
	}
}

func TestExtractFeaturesNotEnoughBars(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	bars := make([]model.TickBar, 5) // 不够 window(10)+future(3)
	feats := eng.ExtractFeatures(bars)
	if len(feats) != 0 {
		t.Errorf("expected 0 features for insufficient bars, got %d", len(feats))
	}
}

func TestExtractFeaturesWithMeta(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	ticks := makeTicks(500)
	bars := eng.AggregateBars(ticks)

	feats := eng.ExtractFeaturesWithMeta(bars, "2018-05-18", "002484")
	if len(feats) == 0 {
		t.Fatal("expected features > 0")
	}
	for _, f := range feats {
		if f.Date != "2018-05-18" {
			t.Errorf("date = %q, want 2018-05-18", f.Date)
		}
		if f.Symbol != "002484" {
			t.Errorf("symbol = %q, want 002484", f.Symbol)
		}
	}
}

func TestExportFeaturesCSV(t *testing.T) {
	eng := NewEngineer(30, 10, 3, 0.02)
	ticks := makeTicks(500)
	bars := eng.AggregateBars(ticks)
	feats := eng.ExtractFeaturesWithMeta(bars, "2018-05-18", "002484")

	dir := t.TempDir()
	path := filepath.Join(dir, "features.csv")

	if err := ExportFeaturesCSV(feats, path); err != nil {
		t.Fatalf("ExportFeaturesCSV: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)
	// 应该包含表头
	if len(content) < 100 {
		t.Error("CSV content too short")
	}
}

func TestExportFeaturesCSVEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.csv")
	err := ExportFeaturesCSV(nil, path)
	if err == nil {
		t.Error("expected error for empty features")
	}
}
