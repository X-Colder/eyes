package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eyes/internal/engine"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/loader"
	"github.com/eyes/internal/model"
)

// 模拟测试配置
type SimulationConfig struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Mode        string  `json:"mode"` // Mock服务模式
	ErrorRate   float64 `json:"error_rate"`
	InitialCash float64 `json:"initial_cash"`
	Commission  float64 `json:"commission"`
	Slippage    float64 `json:"slippage"`
	MaxPosition int64   `json:"max_position"`
	BarInterval int     `json:"bar_interval"`
	WindowSize  int     `json:"window_size"`
	FutureSteps int     `json:"future_steps"`
	PriceThresh float64 `json:"price_thresh"`
}

// 测试结果
type SimulationResult struct {
	Config       SimulationConfig    `json:"config"`
	StartTime    time.Time           `json:"start_time"`
	EndTime      time.Time           `json:"end_time"`
	Duration     time.Duration       `json:"duration"`
	DayCount     int                 `json:"day_count"`
	BarCount     int                 `json:"bar_count"`
	SignalCount  int                 `json:"signal_count"`
	TradeCount   int                 `json:"trade_count"`
	WinRate      float64             `json:"win_rate"`
	TotalPnL     float64             `json:"total_pnl"`
	TotalReturn  float64             `json:"total_return"`
	MaxDrawdown  float64             `json:"max_drawdown"`
	DailyResults []model.DayResult   `json:"daily_results"`
	Trades       []model.TradeRecord `json:"trades"`
}

// TestSimulation 真实场景模拟测试
func TestSimulation(t *testing.T) {
	// 加载测试配置
	config := SimulationConfig{
		Name:        "基础功能测试",
		Description: "正常市场环境下的基础交易逻辑测试",
		Mode:        "normal",
		ErrorRate:   0.0,
		InitialCash: 100000,
		Commission:  0.0003,
		Slippage:    0.001,
		MaxPosition: 10000,
		BarInterval: 30,
		WindowSize:  10,
		FutureSteps: 3,
		PriceThresh: 0.02,
	}

	// 运行模拟测试
	result, err := RunSimulation(config, "../002484.csv")
	if err != nil {
		t.Fatalf("模拟测试失败: %v", err)
	}

	// 保存结果
	saveResult(result, "simulation_result.json")

	// 基础断言
	if result.TotalReturn < -50 {
		t.Errorf("总收益率过低: %.2f%%", result.TotalReturn)
	}
	if result.TradeCount == 0 {
		t.Error("没有产生任何交易")
	}

	log.Printf("模拟测试完成: 收益率=%.2f%%, 交易次数=%d, 胜率=%.1f%%",
		result.TotalReturn, result.TradeCount, result.WinRate)
}

// TestBullMarket 牛市场景测试
func TestBullMarket(t *testing.T) {
	config := SimulationConfig{
		Name:        "牛市测试",
		Description: "牛市环境下的策略表现测试",
		Mode:        "bull",
		ErrorRate:   0.0,
		InitialCash: 100000,
		Commission:  0.0003,
		Slippage:    0.001,
		MaxPosition: 10000,
		BarInterval: 30,
		WindowSize:  10,
		FutureSteps: 3,
		PriceThresh: 0.02,
	}

	result, err := RunSimulation(config, "../002484.csv")
	if err != nil {
		t.Fatalf("牛市测试失败: %v", err)
	}

	saveResult(result, "bull_market_result.json")
	log.Printf("牛市测试完成: 收益率=%.2f%%, 交易次数=%d, 胜率=%.1f%%",
		result.TotalReturn, result.TradeCount, result.WinRate)
}

// TestBearMarket 熊市场景测试
func TestBearMarket(t *testing.T) {
	config := SimulationConfig{
		Name:        "熊市测试",
		Description: "熊市环境下的策略表现测试",
		Mode:        "bear",
		ErrorRate:   0.0,
		InitialCash: 100000,
		Commission:  0.0003,
		Slippage:    0.001,
		MaxPosition: 10000,
		BarInterval: 30,
		WindowSize:  10,
		FutureSteps: 3,
		PriceThresh: 0.02,
	}

	result, err := RunSimulation(config, "../002484.csv")
	if err != nil {
		t.Fatalf("熊市测试失败: %v", err)
	}

	saveResult(result, "bear_market_result.json")
	log.Printf("熊市测试完成: 收益率=%.2f%%, 交易次数=%d, 胜率=%.1f%%",
		result.TotalReturn, result.TradeCount, result.WinRate)
}

// TestErrorInjection 错误注入测试
func TestErrorInjection(t *testing.T) {
	config := SimulationConfig{
		Name:        "错误注入测试",
		Description: "推理服务高错误率下的系统稳定性测试",
		Mode:        "error",
		ErrorRate:   0.3,
		InitialCash: 100000,
		Commission:  0.0003,
		Slippage:    0.001,
		MaxPosition: 10000,
		BarInterval: 30,
		WindowSize:  10,
		FutureSteps: 3,
		PriceThresh: 0.02,
	}

	result, err := RunSimulation(config, "../002484.csv")
	if err != nil {
		t.Fatalf("错误注入测试失败: %v", err)
	}

	saveResult(result, "error_injection_result.json")
	log.Printf("错误注入测试完成: 收益率=%.2f%%, 交易次数=%d",
		result.TotalReturn, result.TradeCount)
}

// RunSimulation 执行完整模拟测试
func RunSimulation(config SimulationConfig, csvPath string) (*SimulationResult, error) {
	startTime := time.Now()

	// 1. 初始化特征工程器
	eng := feature.NewEngineer(
		config.BarInterval,
		config.WindowSize,
		config.FutureSteps,
		config.PriceThresh,
	)

	// 2. 初始化信号引擎
	se := engine.NewSignalEngine(
		eng,
		"http://localhost:5000",
		config.InitialCash,
		config.Commission,
		config.Slippage,
		config.MaxPosition,
	)

	// 3. 加载历史tick数据
	ticks, err := loader.LoadTickCSV(csvPath)
	if err != nil {
		return nil, fmt.Errorf("加载数据失败: %w", err)
	}

	// 4. 处理数据
	bars := eng.AggregateBars(ticks)
	signals, trades := se.ProcessDayTicks("2018-05-18", "002484", ticks)

	// 5. 计算统计指标
	totalPnL := 0.0
	wins := 0
	for _, trade := range trades {
		totalPnL += trade.PnL
		if trade.PnL > 0 {
			wins++
		}
	}

	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(wins) / float64(len(trades)) * 100
	}

	totalReturn := 0.0
	if config.InitialCash > 0 {
		totalReturn = totalPnL / config.InitialCash * 100
	}

	// 6. 构建结果
	result := &SimulationResult{
		Config:      config,
		StartTime:   startTime,
		EndTime:     time.Now(),
		Duration:    time.Since(startTime),
		DayCount:    1,
		BarCount:    len(bars),
		SignalCount: len(signals),
		TradeCount:  len(trades),
		WinRate:     winRate,
		TotalPnL:    totalPnL,
		TotalReturn: totalReturn,
		MaxDrawdown: calculateMaxDrawdown(trades, config.InitialCash),
		Trades:      trades,
	}

	return result, nil
}

// calculateMaxDrawdown 计算最大回撤
func calculateMaxDrawdown(trades []model.TradeRecord, initialCash float64) float64 {
	if len(trades) == 0 {
		return 0.0
	}

	// 计算权益曲线
	equity := []float64{initialCash}
	currentCash := initialCash
	peak := initialCash
	maxDD := 0.0

	for _, trade := range trades {
		currentCash += trade.PnL
		equity = append(equity, currentCash)

		if currentCash > peak {
			peak = currentCash
		}

		dd := (peak - currentCash) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}

	return maxDD * 100 // 转为百分比
}

// saveResult 保存测试结果到文件
func saveResult(result *SimulationResult, filename string) error {
	os.MkdirAll("results", 0755)
	path := filepath.Join("results", filename)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
