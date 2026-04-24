package monitor

import (
	"encoding/json"
	"net/http"
	"time"
)

// MonitorAPI 监控API服务
type MonitorAPI struct {
	collector *MonitorCollector
	server    *http.Server
}

// NewMonitorAPI 创建监控API
func NewMonitorAPI(collector *MonitorCollector, port string) *MonitorAPI {
	api := &MonitorAPI{
		collector: collector,
	}

	mux := http.NewServeMux()

	// 注册路由
	mux.HandleFunc("/api/dashboard", api.handleDashboard)
	mux.HandleFunc("/api/training", api.handleTraining)
	mux.HandleFunc("/api/trading", api.handleTrading)
	mux.HandleFunc("/api/returns", api.handleReturns)
	mux.HandleFunc("/api/risk", api.handleRisk)
	mux.HandleFunc("/api/signals", api.handleSignals)
	mux.HandleFunc("/api/trades", api.handleTrades)
	mux.HandleFunc("/api/equity", api.handleEquity)
	mux.HandleFunc("/api/correlations", api.handleCorrelations)
	mux.HandleFunc("/api/system", api.handleSystem)
	mux.HandleFunc("/health", api.handleHealth)

	api.server = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	return api
}

// Start 启动监控服务
func (api *MonitorAPI) Start() error {
	return api.server.ListenAndServe()
}

// handleDashboard 仪表盘总览
func (api *MonitorAPI) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data := api.collector.GetDashboardData()
	api.jsonResponse(w, data)
}

// handleTraining 训练监控
func (api *MonitorAPI) handleTraining(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	response := map[string]interface{}{
		"metrics": api.collector.trainingMetrics,
		"history": api.collector.trainingHistory,
	}

	api.jsonResponse(w, response)
}

// handleTrading 交易监控
func (api *MonitorAPI) handleTrading(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	response := map[string]interface{}{
		"metrics": api.collector.tradingMetrics,
	}

	api.jsonResponse(w, response)
}

// handleReturns 收益监控
func (api *MonitorAPI) handleReturns(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	response := map[string]interface{}{
		"metrics":       api.collector.returnMetrics,
		"equity_curve":  api.collector.equityCurve,
		"daily_returns": api.collector.dailyReturns,
	}

	api.jsonResponse(w, response)
}

// handleRisk 风险监控
func (api *MonitorAPI) handleRisk(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	response := map[string]interface{}{
		"metrics": api.collector.riskMetrics,
		"history": api.collector.riskHistory,
	}

	api.jsonResponse(w, response)
}

// handleSignals 信号记录
func (api *MonitorAPI) handleSignals(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	api.jsonResponse(w, api.collector.recentSignals)
}

// handleTrades 交易记录
func (api *MonitorAPI) handleTrades(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	api.jsonResponse(w, api.collector.recentTrades)
}

// handleEquity 权益曲线
func (api *MonitorAPI) handleEquity(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	api.jsonResponse(w, api.collector.equityCurve)
}

// handleCorrelations 相关性矩阵
func (api *MonitorAPI) handleCorrelations(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	api.jsonResponse(w, api.collector.correlationMap)
}

// handleSystem 系统状态
func (api *MonitorAPI) handleSystem(w http.ResponseWriter, r *http.Request) {
	api.collector.mu.RLock()
	defer api.collector.mu.RUnlock()

	api.jsonResponse(w, api.collector.systemStatus)
}

// handleHealth 健康检查
func (api *MonitorAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	}
	api.jsonResponse(w, response)
}

// jsonResponse JSON响应
func (api *MonitorAPI) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}
