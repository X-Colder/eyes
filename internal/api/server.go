package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/eyes/internal/analysis"
	"github.com/eyes/internal/backtest"
	"github.com/eyes/internal/config"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/loader"
	"github.com/eyes/internal/model"
)

// Server HTTP API 服务
type Server struct {
	cfg      *config.Config
	mux      *http.ServeMux
	bars     []model.TickBar
	feats    []model.Feature
	ticks    []model.TickData
	multiDay *model.MultiDayData // 多日数据
}

// NewServer 创建 API 服务
func NewServer(cfg *config.Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/load", s.handleLoad)
	s.mux.HandleFunc("/api/load-all", s.handleLoadAll)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	s.mux.HandleFunc("/api/bars", s.handleBars)
	s.mux.HandleFunc("/api/features", s.handleFeatures)
	s.mux.HandleFunc("/api/export", s.handleExport)
	s.mux.HandleFunc("/api/train", s.handleTrain)
	s.mux.HandleFunc("/api/predict", s.handlePredict)
	s.mux.HandleFunc("/api/backtest", s.handleBacktest)
}

// Start 启动 HTTP 服务
func (s *Server) Start() error {
	addr := ":" + s.cfg.Server.Port
	log.Printf("[api] server starting on %s", addr)
	return http.ListenAndServe(addr, s.withCORS(s.mux))
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleLoad(w http.ResponseWriter, r *http.Request) {
	csvFile := r.URL.Query().Get("file")
	if csvFile == "" {
		csvFile = filepath.Join(s.cfg.Data.TickDir, "002484.csv")
	}

	ticks, err := loader.LoadTickCSV(csvFile)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("load csv: %v", err))
		return
	}
	s.ticks = ticks

	// 聚合 bar
	eng := feature.NewEngineer(
		s.cfg.Feature.BarInterval,
		s.cfg.Feature.WindowSize,
		s.cfg.Feature.FutureSteps,
		s.cfg.Feature.PriceThresh,
	)
	s.bars = eng.AggregateBars(ticks)

	// 提取特征
	s.feats = eng.ExtractFeatures(s.bars)

	// 趋势标注
	ta := analysis.NewTrendAnalyzer(5, 0.02)
	s.feats = ta.LabelFeatures(s.feats, s.bars)

	stats := loader.GetDailyStats(ticks, "002484")

	writeJSON(w, map[string]interface{}{
		"ticks":    len(ticks),
		"bars":     len(s.bars),
		"features": len(s.feats),
		"stats":    stats,
	})
}

// handleLoadAll 批量加载目录下所有 CSV 文件（多日数据）
func (s *Server) handleLoadAll(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = s.cfg.Data.TickDir
	}
	symbol := r.URL.Query().Get("symbol")
	defaultDate := r.URL.Query().Get("default_date")

	dayTicks, err := loader.LoadMultiDayDir(dir, symbol, defaultDate)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("load multi-day: %v", err))
		return
	}
	if len(dayTicks) == 0 {
		writeError(w, 400, "no CSV files found")
		return
	}

	eng := feature.NewEngineer(
		s.cfg.Feature.BarInterval,
		s.cfg.Feature.WindowSize,
		s.cfg.Feature.FutureSteps,
		s.cfg.Feature.PriceThresh,
	)
	ta := analysis.NewTrendAnalyzer(5, 0.02)

	multiDay := &model.MultiDayData{
		Symbol:  symbol,
		DayBars: make(map[string][]model.TickBar),
	}

	for _, dt := range dayTicks {
		multiDay.Days = append(multiDay.Days, dt.Date)
		bars := eng.AggregateBars(dt.Ticks)
		multiDay.DayBars[dt.Date] = bars
		multiDay.AllBars = append(multiDay.AllBars, bars...)

		feats := eng.ExtractFeaturesWithMeta(bars, dt.Date, dt.Symbol)
		feats = ta.LabelFeatures(feats, bars)
		multiDay.Features = append(multiDay.Features, feats...)

		stats := loader.GetDailyStats(dt.Ticks, dt.Symbol)
		multiDay.Stats = append(multiDay.Stats, stats)
	}

	s.multiDay = multiDay
	s.bars = multiDay.AllBars
	s.feats = multiDay.Features

	// 合并所有 ticks
	s.ticks = nil
	for _, dt := range dayTicks {
		s.ticks = append(s.ticks, dt.Ticks...)
	}

	writeJSON(w, map[string]interface{}{
		"days":       len(multiDay.Days),
		"dates":      multiDay.Days,
		"total_bars": len(multiDay.AllBars),
		"features":   len(multiDay.Features),
		"stats":      multiDay.Stats,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if len(s.ticks) == 0 {
		writeError(w, 400, "data not loaded, call /api/load first")
		return
	}
	stats := loader.GetDailyStats(s.ticks, "002484")
	writeJSON(w, stats)
}

func (s *Server) handleBars(w http.ResponseWriter, r *http.Request) {
	if len(s.bars) == 0 {
		writeError(w, 400, "data not loaded")
		return
	}
	writeJSON(w, s.bars)
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	if len(s.feats) == 0 {
		writeError(w, 400, "features not computed")
		return
	}
	// 返回摘要，不返回全部特征值（数据量太大）
	summary := make([]map[string]interface{}, len(s.feats))
	for i, f := range s.feats {
		summary[i] = map[string]interface{}{
			"date":      f.Date,
			"symbol":    f.Symbol,
			"time":      f.Time,
			"label":     f.Label,
			"price_chg": f.PriceChg,
			"phase":     f.Phase.String(),
			"dim":       len(f.Values),
		}
	}
	writeJSON(w, summary)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if len(s.feats) == 0 {
		writeError(w, 400, "features not computed, call /api/load first")
		return
	}

	os.MkdirAll(s.cfg.Data.OutputDir, 0755)

	// 导出特征 CSV（给 Python 训练用）
	csvPath := filepath.Join(s.cfg.Data.OutputDir, "features.csv")
	if err := feature.ExportFeaturesCSV(s.feats, csvPath); err != nil {
		writeError(w, 500, fmt.Sprintf("export csv: %v", err))
		return
	}

	// 导出完整 JSON
	jsonPath := filepath.Join(s.cfg.Data.OutputDir, "data.json")
	stats := loader.GetDailyStats(s.ticks, "002484")
	exportData := &feature.ExportData{
		Symbol:     "002484",
		Date:       "2018-05-18",
		BarCount:   len(s.bars),
		FeatureDim: len(s.feats[0].Values),
		WindowSize: s.cfg.Feature.WindowSize,
		Bars:       s.bars,
		Features:   s.feats,
		Stats:      stats,
	}
	if err := feature.ExportToJSON(exportData, jsonPath); err != nil {
		writeError(w, 500, fmt.Sprintf("export json: %v", err))
		return
	}

	writeJSON(w, map[string]string{
		"features_csv": csvPath,
		"data_json":    jsonPath,
		"status":       "exported",
	})
}

func (s *Server) handleTrain(w http.ResponseWriter, r *http.Request) {
	// 先确保数据已导出
	if len(s.feats) == 0 {
		writeError(w, 400, "features not computed, call /api/load first")
		return
	}

	scriptPath := filepath.Join(s.cfg.ML.ScriptDir, "train.py")
	dataPath := filepath.Join(s.cfg.Data.OutputDir, "features.csv")
	modelDir := s.cfg.ML.ModelDir

	os.MkdirAll(modelDir, 0755)

	cmd := exec.Command(s.cfg.ML.PythonPath, scriptPath,
		"--data", dataPath,
		"--model-dir", modelDir,
		"--window-size", fmt.Sprintf("%d", s.cfg.Feature.WindowSize),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("[api] starting training: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		writeError(w, 500, fmt.Sprintf("start training: %v", err))
		return
	}

	writeJSON(w, map[string]interface{}{
		"status": "training_started",
		"pid":    cmd.Process.Pid,
		"script": scriptPath,
	})
}

func (s *Server) handlePredict(w http.ResponseWriter, r *http.Request) {
	// 调用 Python 推理服务
	if s.cfg.ML.ServiceURL == "" {
		writeError(w, 400, "ml service url not configured")
		return
	}

	resp, err := http.Get(s.cfg.ML.ServiceURL + "/predict")
	if err != nil {
		writeError(w, 500, fmt.Sprintf("call ml service: %v", err))
		return
	}
	defer resp.Body.Close()

	var predictions []model.PredictionResult
	if err := json.NewDecoder(resp.Body).Decode(&predictions); err != nil {
		writeError(w, 500, fmt.Sprintf("decode predictions: %v", err))
		return
	}

	writeJSON(w, predictions)
}

func (s *Server) handleBacktest(w http.ResponseWriter, r *http.Request) {
	if len(s.bars) == 0 {
		writeError(w, 400, "data not loaded")
		return
	}

	// 使用理想标签进行回测（完美策略上限）
	ta := analysis.NewTrendAnalyzer(5, 0.02)
	labels := ta.GenerateTradeLabels(s.bars)

	engine := backtest.NewEngine(
		s.cfg.Backtest.InitialCash,
		s.cfg.Backtest.Commission,
		s.cfg.Backtest.Slippage,
		s.cfg.Backtest.MaxPosition,
	)

	result := engine.RunWithLabels(s.bars, labels)
	result.Symbol = "002484"
	writeJSON(w, result)
}
