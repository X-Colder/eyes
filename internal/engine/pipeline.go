package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/eyes/internal/analysis"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/loader"
	"github.com/eyes/internal/model"
)

// PipelineConfig 闭环流水线配置
type PipelineConfig struct {
	Symbol       string  `json:"symbol"`
	TickDir      string  `json:"tick_dir"`      // tick CSV 目录
	OutputDir    string  `json:"output_dir"`    // 特征输出目录
	ModelDir     string  `json:"model_dir"`     // 模型目录
	ScriptDir    string  `json:"script_dir"`    // Python 脚本目录
	PythonPath   string  `json:"python_path"`   // python3 路径
	ServiceURL   string  `json:"service_url"`   // 推理服务地址
	TrainRatio   float64 `json:"train_ratio"`   // 训练天数占比 (如 0.7)
	InitialCash  float64 `json:"initial_cash"`  // 初始资金
	Commission   float64 `json:"commission"`    // 手续费率
	Slippage     float64 `json:"slippage"`      // 滑点
	MaxPosition  int64   `json:"max_position"`  // 最大持仓
	BarInterval  int     `json:"bar_interval"`  // bar 秒数
	WindowSize   int     `json:"window_size"`   // 特征窗口
	FutureSteps  int     `json:"future_steps"`  // 预测步数
	PriceThresh  float64 `json:"price_thresh"`  // 涨跌阈值
	RetrainAfter int     `json:"retrain_after"` // 每推理 N 天后再训练
	FeatureDim   int     `json:"feature_dim"`   // 特征维度 (24)
}

// Pipeline 闭环流水线：训练 → 推理交易 → 回测 → 再训练
type Pipeline struct {
	cfg   PipelineConfig
	state model.PipelineState
	eng   *feature.Engineer
	ta    *analysis.TrendAnalyzer
}

// NewPipeline 创建闭环流水线
func NewPipeline(cfg PipelineConfig) *Pipeline {
	return &Pipeline{
		cfg: cfg,
		eng: feature.NewEngineer(cfg.BarInterval, cfg.WindowSize, cfg.FutureSteps, cfg.PriceThresh),
		ta:  analysis.NewTrendAnalyzer(5, 0.02),
		state: model.PipelineState{
			Symbol:       cfg.Symbol,
			Phase:        "idle",
			Cash:         cfg.InitialCash,
			TotalValue:   cfg.InitialCash,
			ModelVersion: 0,
		},
	}
}

// RunResult 流水线运行结果
type RunResult struct {
	State        model.PipelineState `json:"state"`
	TrainDays    []string            `json:"train_days"`
	InferDays    []string            `json:"infer_days"`
	TotalSignals int                 `json:"total_signals"`
	TotalTrades  int                 `json:"total_trades"`
	FinalCash    float64             `json:"final_cash"`
	TotalPnL     float64             `json:"total_pnl"`
	TotalReturn  float64             `json:"total_return"`
	DayResults   []model.DayResult   `json:"day_results"`
	Error        string              `json:"error,omitempty"`
}

// Run 执行完整闭环流程
// 1. 加载所有天数据，按 trainRatio 分为训练天/推理天
// 2. 用训练天数据导出特征 → 训练模型
// 3. 启动推理服务 → 逐天推理并交易
// 4. 每 retrainAfter 天追加数据再训练
// 5. 返回完整结果
func (p *Pipeline) Run() RunResult {
	log.Printf("[pipeline] starting pipeline for %s", p.cfg.Symbol)

	// 1. 加载数据
	dayTicks, err := loader.LoadMultiDayDir(p.cfg.TickDir, p.cfg.Symbol, "")
	if err != nil {
		return RunResult{Error: fmt.Sprintf("load data: %v", err)}
	}
	if len(dayTicks) < 2 {
		return RunResult{Error: fmt.Sprintf("need at least 2 days, got %d", len(dayTicks))}
	}

	// 按日期排序
	sort.Slice(dayTicks, func(i, j int) bool { return dayTicks[i].Date < dayTicks[j].Date })
	allDates := make([]string, len(dayTicks))
	dateToTicks := make(map[string]model.DayTicks)
	for i, dt := range dayTicks {
		allDates[i] = dt.Date
		dateToTicks[dt.Date] = dt
	}
	log.Printf("[pipeline] loaded %d days: %v", len(allDates), allDates)

	// 2. 划分训练/推理天
	splitIdx := int(float64(len(allDates)) * p.cfg.TrainRatio)
	if splitIdx < 1 {
		splitIdx = 1
	}
	if splitIdx >= len(allDates) {
		splitIdx = len(allDates) - 1
	}
	trainDays := allDates[:splitIdx]
	inferDays := allDates[splitIdx:]

	p.state.TrainDays = trainDays
	p.state.InferDays = inferDays
	log.Printf("[pipeline] train days: %v, infer days: %v", trainDays, inferDays)

	// 3. 初始训练
	p.state.Phase = "train"
	if err := p.trainModel(trainDays, dateToTicks); err != nil {
		return RunResult{Error: fmt.Sprintf("initial train: %v", err)}
	}
	p.state.ModelVersion = 1

	// 4. 等待推理服务就绪
	p.state.Phase = "infer"
	if !p.waitForService(30 * time.Second) {
		return RunResult{Error: "inference service not ready after 30s"}
	}

	// 5. 逐天推理+交易
	signalEngine := NewSignalEngine(
		p.eng, p.cfg.ServiceURL, p.state.Cash,
		p.cfg.Commission, p.cfg.Slippage, p.cfg.MaxPosition,
	)

	inferCount := 0
	for _, date := range inferDays {
		p.state.CurrentDay = date
		dt := dateToTicks[date]

		daySignals, dayTrades := signalEngine.ProcessDayTicks(date, dt.Symbol, dt.Ticks)

		// 日终强制平仓
		pos, cash, _ := signalEngine.GetState()
		if pos.Side == "long" && len(dt.Ticks) > 0 {
			lastPrice := dt.Ticks[len(dt.Ticks)-1].Price
			trade := signalEngine.ForceClose(lastPrice, dt.Ticks[len(dt.Ticks)-1].Time)
			if trade != nil {
				dayTrades = append(dayTrades, *trade)
			}
			_, cash, _ = signalEngine.GetState()
		}

		// 统计当日结果
		dayResult := p.buildDayResult(date, daySignals, dayTrades)
		p.state.DailyResults = append(p.state.DailyResults, dayResult)
		p.state.Cash = cash
		p.state.CumulativePnL += dayResult.DayPnL

		log.Printf("[pipeline] day %s: signals=%d trades=%d pnl=%.2f cash=%.2f",
			date, dayResult.SignalCount, dayResult.TradeCount, dayResult.DayPnL, cash)

		inferCount++

		// 检查是否需要再训练
		if p.cfg.RetrainAfter > 0 && inferCount%p.cfg.RetrainAfter == 0 && date != inferDays[len(inferDays)-1] {
			log.Printf("[pipeline] retrain triggered after %d infer days", inferCount)
			p.state.Phase = "retrain"
			// 追加已推理的日期到训练集
			retrainDays := append(trainDays, inferDays[:inferCount]...)
			if err := p.trainModel(retrainDays, dateToTicks); err != nil {
				log.Printf("[pipeline] retrain error: %v", err)
			} else {
				p.state.ModelVersion++
				trainDays = retrainDays
			}
			p.state.Phase = "infer"
			// 等待新模型加载
			time.Sleep(2 * time.Second)
		}
	}

	// 6. 汇总结果
	_, finalCash, allTrades := signalEngine.GetState()
	p.state.Cash = finalCash
	p.state.Trades = allTrades
	p.state.TotalValue = finalCash
	p.state.Phase = "done"

	totalReturn := 0.0
	if p.cfg.InitialCash > 0 {
		totalReturn = (finalCash - p.cfg.InitialCash) / p.cfg.InitialCash * 100
	}

	return RunResult{
		State:        p.state,
		TrainDays:    trainDays,
		InferDays:    inferDays,
		TotalSignals: len(p.state.Signals),
		TotalTrades:  len(allTrades),
		FinalCash:    finalCash,
		TotalPnL:     p.state.CumulativePnL,
		TotalReturn:  totalReturn,
		DayResults:   p.state.DailyResults,
	}
}

// GetState 获取当前状态
func (p *Pipeline) GetState() model.PipelineState {
	return p.state
}

// --- 内部方法 ---

// trainModel 导出训练数据并启动训练
func (p *Pipeline) trainModel(days []string, dateToTicks map[string]model.DayTicks) error {
	log.Printf("[pipeline] training with %d days", len(days))

	os.MkdirAll(p.cfg.OutputDir, 0755)
	os.MkdirAll(p.cfg.ModelDir, 0755)

	// 导出训练特征
	var allFeatures []model.Feature
	for _, date := range days {
		dt := dateToTicks[date]
		bars := p.eng.AggregateBars(dt.Ticks)
		feats := p.eng.ExtractFeaturesWithMeta(bars, date, dt.Symbol)
		feats = p.ta.LabelFeatures(feats, bars)
		allFeatures = append(allFeatures, feats...)
	}

	csvPath := filepath.Join(p.cfg.OutputDir, "pipeline_features.csv")
	if err := feature.ExportFeaturesCSV(allFeatures, csvPath); err != nil {
		return fmt.Errorf("export features: %w", err)
	}
	log.Printf("[pipeline] exported %d features to %s", len(allFeatures), csvPath)

	// 调用训练脚本
	scriptPath := filepath.Join(p.cfg.ScriptDir, "train.py")
	cmd := exec.Command(p.cfg.PythonPath, scriptPath,
		"--data", csvPath,
		"--model-dir", p.cfg.ModelDir,
		"--window-size", fmt.Sprintf("%d", p.cfg.WindowSize),
		"--epochs", "30",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("[pipeline] running: %s", cmd.String())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("train: %w", err)
	}
	log.Printf("[pipeline] training completed (model v%d)", p.state.ModelVersion+1)
	return nil
}

// waitForService 等待推理服务就绪
func (p *Pipeline) waitForService(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(p.cfg.ServiceURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Printf("[pipeline] inference service ready")
				return true
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

// buildDayResult 构建单日结果
func (p *Pipeline) buildDayResult(date string, signals []model.TradeSignal, trades []model.TradeRecord) model.DayResult {
	buySignals, sellSignals := 0, 0
	for _, s := range signals {
		switch s.Action {
		case "buy":
			buySignals++
		case "sell":
			sellSignals++
		}
	}

	dayPnL := 0.0
	wins := 0
	for _, t := range trades {
		dayPnL += t.PnL
		if t.PnL > 0 {
			wins++
		}
	}
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(wins) / float64(len(trades)) * 100
	}
	dayReturn := 0.0
	if p.state.Cash > 0 {
		dayReturn = dayPnL / p.state.Cash * 100
	}

	return model.DayResult{
		Date:        date,
		SignalCount: len(signals),
		BuySignals:  buySignals,
		SellSignals: sellSignals,
		TradeCount:  len(trades),
		DayPnL:      dayPnL,
		DayReturn:   dayReturn,
		WinRate:     winRate,
		Trades:      trades,
	}
}

// ToJSON 序列化状态
func (p *Pipeline) ToJSON() ([]byte, error) {
	return json.MarshalIndent(p.state, "", "  ")
}
