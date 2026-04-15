package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValid(t *testing.T) {
	content := `{
		"server": {"port": "8080"},
		"data": {"tick_dir": ".", "output_dir": "data/features"},
		"feature": {"bar_interval": 30, "window_size": 10, "future_steps": 3, "price_thresh": 0.02},
		"ml": {"model_dir": "data/models", "script_dir": "ml/scripts", "python_path": "python3", "service_url": "http://localhost:5000"},
		"backtest": {"initial_cash": 100000, "commission": 0.0003, "slippage": 0.001, "max_position": 10000},
		"pipeline": {"symbol": "002484", "train_ratio": 0.7, "retrain_after": 3, "feature_dim": 24}
	}`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("port = %q, want 8080", cfg.Server.Port)
	}
	if cfg.Feature.BarInterval != 30 {
		t.Errorf("bar_interval = %d, want 30", cfg.Feature.BarInterval)
	}
	if cfg.Feature.WindowSize != 10 {
		t.Errorf("window_size = %d, want 10", cfg.Feature.WindowSize)
	}
	if cfg.Backtest.InitialCash != 100000 {
		t.Errorf("initial_cash = %.0f, want 100000", cfg.Backtest.InitialCash)
	}
	if cfg.Pipeline.Symbol != "002484" {
		t.Errorf("pipeline.symbol = %q, want 002484", cfg.Pipeline.Symbol)
	}
	if cfg.Pipeline.TrainRatio != 0.7 {
		t.Errorf("pipeline.train_ratio = %f, want 0.7", cfg.Pipeline.TrainRatio)
	}
	if cfg.Pipeline.RetrainAfter != 3 {
		t.Errorf("pipeline.retrain_after = %d, want 3", cfg.Pipeline.RetrainAfter)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.json")
	os.WriteFile(path, []byte("{invalid}"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadPartial(t *testing.T) {
	content := `{"server": {"port": "9090"}}`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "partial.json")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != "9090" {
		t.Errorf("port = %q, want 9090", cfg.Server.Port)
	}
	if cfg.Feature.BarInterval != 0 {
		t.Errorf("bar_interval should default to 0, got %d", cfg.Feature.BarInterval)
	}
}
