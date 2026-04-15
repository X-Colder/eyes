package loader

import (
	"os"
	"path/filepath"
	"testing"
)

const testCSVHeader = "TranID,Time,Price,Volume,SaleOrderVolume,BuyOrderVolume,Type,SaleOrderID,SaleOrderPrice,BuyOrderID,BuyOrderPrice\n"

func writeTestCSV(t *testing.T, dir, name, rows string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := testCSVHeader + rows
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

func TestLoadTickCSV(t *testing.T) {
	dir := t.TempDir()
	rows := "172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84\n" +
		"172292,09:25:00,7.84,600,2500,600,S,162351,7.84,139236,7.84\n" +
		"219201,09:30:00,7.85,100,500,100,S,219200,7.85,180733,7.85\n" +
		"257453,09:30:02,7.85,400,500,400,B,219200,7.85,257452,7.85\n"
	path := writeTestCSV(t, dir, "002484.csv", rows)

	ticks, err := LoadTickCSV(path)
	if err != nil {
		t.Fatalf("LoadTickCSV: %v", err)
	}
	if len(ticks) != 4 {
		t.Fatalf("expected 4 ticks, got %d", len(ticks))
	}
	// 验证排序
	if ticks[0].Time > ticks[len(ticks)-1].Time {
		t.Error("ticks not sorted by time")
	}
	// 验证字段
	if ticks[0].TranID != 172291 {
		t.Errorf("first tick TranID = %d, want 172291", ticks[0].TranID)
	}
	if ticks[0].Price != 7.84 {
		t.Errorf("first tick Price = %f, want 7.84", ticks[0].Price)
	}
	if ticks[3].Type != "B" {
		t.Errorf("last tick Type = %q, want B", ticks[3].Type)
	}
}

func TestLoadTickCSVMissing(t *testing.T) {
	_, err := LoadTickCSV("/nonexistent/file.csv")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadTickCSVSkipBadRows(t *testing.T) {
	dir := t.TempDir()
	rows := "172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84\n" +
		"bad,row,only,three,cols\n" +
		"172292,09:25:00,7.84,600,2500,600,S,162351,7.84,139236,7.84\n"
	path := writeTestCSV(t, dir, "002484.csv", rows)

	ticks, err := LoadTickCSV(path)
	if err != nil {
		t.Fatalf("LoadTickCSV: %v", err)
	}
	if len(ticks) != 2 {
		t.Errorf("expected 2 valid ticks (skipping bad row), got %d", len(ticks))
	}
}

func TestParseFileName(t *testing.T) {
	tests := []struct {
		name       string
		wantSymbol string
		wantDate   string
	}{
		{"002484_2018-05-18.csv", "002484", "2018-05-18"},
		{"002484_20180518.csv", "002484", "2018-05-18"},
		{"002484-2018-05-18.csv", "002484", "2018-05-18"},
		{"002484.csv", "002484", ""},
		{"readme.txt", "", ""},
		{"abc.csv", "", ""},
	}
	for _, tt := range tests {
		sym, date := parseFileName(tt.name)
		if sym != tt.wantSymbol || date != tt.wantDate {
			t.Errorf("parseFileName(%q) = (%q,%q), want (%q,%q)",
				tt.name, sym, date, tt.wantSymbol, tt.wantDate)
		}
	}
}

func TestLoadMultiDayDir(t *testing.T) {
	dir := t.TempDir()
	rows1 := "172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84\n"
	rows2 := "172291,09:25:00,7.90,1000,2000,1000,B,100000,7.90,200000,7.90\n"

	writeTestCSV(t, dir, "002484_2018-05-18.csv", rows1)
	writeTestCSV(t, dir, "002484_2018-05-21.csv", rows2)

	dayTicks, err := LoadMultiDayDir(dir, "002484", "")
	if err != nil {
		t.Fatalf("LoadMultiDayDir: %v", err)
	}
	if len(dayTicks) != 2 {
		t.Fatalf("expected 2 days, got %d", len(dayTicks))
	}
	// 日期排序
	if dayTicks[0].Date != "2018-05-18" {
		t.Errorf("first day = %q, want 2018-05-18", dayTicks[0].Date)
	}
	if dayTicks[1].Date != "2018-05-21" {
		t.Errorf("second day = %q, want 2018-05-21", dayTicks[1].Date)
	}
}

func TestLoadMultiDayDirFilterSymbol(t *testing.T) {
	dir := t.TempDir()
	rows := "172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84\n"
	writeTestCSV(t, dir, "002484_2018-05-18.csv", rows)
	writeTestCSV(t, dir, "600000_2018-05-18.csv", rows)

	dayTicks, err := LoadMultiDayDir(dir, "002484", "")
	if err != nil {
		t.Fatalf("LoadMultiDayDir: %v", err)
	}
	if len(dayTicks) != 1 {
		t.Errorf("expected 1 day (filtered), got %d", len(dayTicks))
	}
}

func TestGetDailyStats(t *testing.T) {
	dir := t.TempDir()
	rows := "172291,09:25:00,7.84,1900,2500,1900,S,162351,7.84,86382,7.84\n" +
		"172292,09:25:00,7.84,600,2500,600,S,162351,7.84,139236,7.84\n" +
		"219201,09:30:00,7.85,100,500,100,S,219200,7.85,180733,7.85\n" +
		"257453,09:30:02,7.80,400,500,400,B,219200,7.80,257452,7.80\n"
	path := writeTestCSV(t, dir, "002484.csv", rows)

	ticks, _ := LoadTickCSV(path)
	stats := GetDailyStats(ticks, "002484")

	if stats.TotalTicks != 4 {
		t.Errorf("TotalTicks = %d, want 4", stats.TotalTicks)
	}
	if stats.OpenPrice != 7.84 {
		t.Errorf("OpenPrice = %.2f, want 7.84", stats.OpenPrice)
	}
	if stats.HighPrice != 7.85 {
		t.Errorf("HighPrice = %.2f, want 7.85", stats.HighPrice)
	}
	if stats.LowPrice != 7.80 {
		t.Errorf("LowPrice = %.2f, want 7.80", stats.LowPrice)
	}
	if stats.Amplitude <= 0 {
		t.Errorf("Amplitude should be > 0, got %.4f", stats.Amplitude)
	}
}
