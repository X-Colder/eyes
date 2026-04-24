package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/eyes/internal/monitor"
)

func main() {
	// 创建监控收集器
	collector := monitor.NewMonitorCollector()

	// 初始化收益数据
	collector.UpdateEquity(1000000, 1000000, 0)

	// 创建报警管理器
	alertConfig := monitor.DefaultAlertConfig()
	alertConfig.EmailEnabled = false // 可根据需要启用
	alertConfig.DingTalkEnabled = false
	alertMgr := monitor.NewAlertManager(alertConfig, collector)

	// 启动报警检查
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			data := collector.GetDashboardData()
			alertMgr.CheckAlerts(data)
		}
	}()

	// 模拟数据更新 (演示用)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		equity := 1000000.0
		epoch := 0

		for range ticker.C {
			// 模拟权益变化
			equity += (0.001 - 0.0005) * equity // 随机波动
			collector.UpdateEquity(equity, equity*0.8, equity*0.2)

			// 模拟训练进度
			epoch++
			if epoch <= 100 {
				collector.UpdateTrainingMetrics(&monitor.TrainingMetrics{
					CurrentEpoch:  epoch,
					TotalEpochs:   100,
					TrainLoss:     0.5 - float64(epoch)*0.003,
					ValLoss:       0.55 - float64(epoch)*0.002,
					TrainAccuracy: 0.6 + float64(epoch)*0.002,
					ValAccuracy:   0.58 + float64(epoch)*0.0018,
					LearningRate:  0.001,
					Progress:      float64(epoch),
					Status:        "running",
					TrainingTime:  float64(epoch * 2),
				})
			}

			// 模拟信号
			if epoch%10 == 0 {
				collector.RecordSignal(&monitor.SignalRecord{
					Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
					Symbol:     "002484",
					Action:     "buy",
					Price:      25.5,
					Volume:     1000,
					Confidence: 0.85,
					Strategy:   "resonance",
				})
			}

			// 模拟交易
			if epoch%15 == 0 {
				collector.RecordTrade(&monitor.TradeRecord{
					Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
					Symbol:     "002484",
					Side:       "sell",
					Price:      26.0,
					Volume:     1000,
					PnL:        500.0,
					PnLPct:     2.0,
					HoldTime:   300.0,
					EntryPrice: 25.5,
					ExitPrice:  26.0,
				})
			}

			// 模拟风险指标
			collector.UpdateRiskMetrics(&monitor.RiskMetrics{
				TotalRisk:       0.15,
				VaR95:           0.08,
				VaR99:           0.12,
				Leverage:        1.5,
				MarginUsed:      150000,
				MarginAvailable: 850000,
			})

			// 模拟系统状态
			collector.UpdateSystemStatus(&monitor.SystemStatus{
				Status:         "running",
				Uptime:         int64(epoch * 5),
				ModelLoaded:    true,
				ModelVersion:   1,
				DataFeedStatus: "connected",
				OrdersPending:  2,
				OrdersFilled:   int64(epoch),
			})
		}
	}()

	// 创建路由器
	mux := http.NewServeMux()

	// API路由 - 使用GetDashboardData方法
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		data := collector.GetDashboardData()
		jsonResponse(w, data)
	})

	mux.HandleFunc("/api/training", func(w http.ResponseWriter, r *http.Request) {
		data := collector.GetDashboardData()
		jsonResponse(w, map[string]interface{}{
			"metrics": data.Training,
			"history": data.TrainingHist,
		})
	})

	mux.HandleFunc("/api/trading", func(w http.ResponseWriter, r *http.Request) {
		data := collector.GetDashboardData()
		jsonResponse(w, map[string]interface{}{
			"metrics": data.Trading,
		})
	})

	mux.HandleFunc("/api/returns", func(w http.ResponseWriter, r *http.Request) {
		data := collector.GetDashboardData()
		jsonResponse(w, map[string]interface{}{
			"metrics":       data.Returns,
			"equity_curve":  data.EquityCurve,
			"daily_returns": data.DailyReturns,
		})
	})

	mux.HandleFunc("/api/risk", func(w http.ResponseWriter, r *http.Request) {
		data := collector.GetDashboardData()
		jsonResponse(w, map[string]interface{}{
			"metrics": data.Risk,
			"history": data.RiskHist,
		})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		})
	})

	// 首页 - 返回index.html
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "internal/monitor/web/index.html")
			return
		}
		// 其他静态文件
		http.FileServer(http.Dir("internal/monitor/web")).ServeHTTP(w, r)
	})

	log.Println("========================================")
	log.Println("  量化交易监控系统启动")
	log.Println("========================================")
	log.Println("监控面板: http://localhost:8082")
	log.Println("API接口:  http://localhost:8082/api/dashboard")
	log.Println("健康检查: http://localhost:8082/health")
	log.Println("========================================")
	log.Println("按 Ctrl+C 停止服务")
	log.Println("========================================")

	if err := http.ListenAndServe(":8082", mux); err != nil {
		log.Fatal("监控服务启动失败:", err)
		os.Exit(1)
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}
