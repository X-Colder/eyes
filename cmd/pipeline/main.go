package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/eyes/internal/analysis"
	"github.com/eyes/internal/backtest"
	"github.com/eyes/internal/config"
	"github.com/eyes/internal/engine"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/loader"
	"github.com/eyes/internal/model"
)

type Pipeline struct {
	cfg               *config.Config
	signalEng         *engine.SignalEngine
	featureEng        *feature.Engineer
	backtestEng       *backtest.Engine
	dataChan          chan *model.TickData
	stopChan          chan struct{}
	trainingData      []*model.TickData
	trainingMu        sync.Mutex
	lastTraining      time.Time
	trainingThreshold int
}

func NewPipeline(cfg *config.Config) *Pipeline {
	featureEng := feature.NewEngineer(
		cfg.Feature.BarInterval,
		cfg.Feature.WindowSize,
		cfg.Feature.FutureSteps,
		cfg.Feature.PriceThresh,
	)
	signalEng := engine.NewSignalEngine(
		featureEng,
		cfg.ML.ServiceURL,
		cfg.Backtest.InitialCash,
		cfg.Backtest.Commission,
		cfg.Backtest.Slippage,
		cfg.Backtest.MaxPosition,
	)
	backtestEng := backtest.NewEngine(
		cfg.Backtest.InitialCash,
		cfg.Backtest.Commission,
		cfg.Backtest.Slippage,
		cfg.Backtest.MaxPosition,
	)

	threshold := 1000 // 默认训练阈值
	return &Pipeline{
		cfg:               cfg,
		signalEng:         signalEng,
		featureEng:        featureEng,
		backtestEng:       backtestEng,
		dataChan:          make(chan *model.TickData, 1000),
		stopChan:          make(chan struct{}),
		trainingThreshold: threshold,
	}
}

func (p *Pipeline) Start() error {
	log.Println("[pipeline] 启动闭环处理服务")

	go p.handleSignals()
	go p.processLoop()
	go p.trainingScheduler()

	log.Println("[pipeline] 服务启动成功，等待tick数据...")
	return nil
}

func (p *Pipeline) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("[pipeline] 收到停止信号，正在优雅关闭...")
	close(p.stopChan)
	close(p.dataChan)
}

func (p *Pipeline) processLoop() {
	for {
		select {
		case tick, ok := <-p.dataChan:
			if !ok {
				return
			}
			p.processTick(tick)
		case <-p.stopChan:
			return
		}
	}
}

func (p *Pipeline) processTick(tick *model.TickData) {
	// 判断是历史数据还是实时数据
	tickTime := tick.Time // 格式 HH:MM:SS
	_ = tickTime
	isHistorical := false // CSV加载的都是历史数据，简化判断

	if isHistorical {
		p.trainingMu.Lock()
		p.trainingData = append(p.trainingData, tick)
		count := len(p.trainingData)
		p.trainingMu.Unlock()

		log.Printf("[pipeline] 历史tick已加入训练集: %s, 总样本量: %d",
			tickTime, count)

		if count >= p.trainingThreshold {
			go p.startTraining()
		}
	} else {
		log.Printf("[pipeline] 处理实时tick: %s, 价格: %.2f, 成交量: %d",
			tickTime, tick.Price, tick.Volume)

		p.trainingMu.Lock()
		p.trainingData = append(p.trainingData, tick)
		p.trainingMu.Unlock()
	}
}

func (p *Pipeline) trainingScheduler() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.trainingMu.Lock()
			count := len(p.trainingData)
			p.trainingMu.Unlock()
			if count > 0 && time.Since(p.lastTraining) > 7*24*time.Hour {
				go p.startTraining()
			}
		case <-p.stopChan:
			return
		}
	}
}

func (p *Pipeline) startTraining() {
	log.Println("[pipeline] 开始模型训练...")

	outputDir := p.cfg.Data.OutputDir
	os.MkdirAll(outputDir, 0755)

	trainingFile := filepath.Join(outputDir, fmt.Sprintf("training_data_%s.csv",
		time.Now().Format("20060102_150405")))

	err := p.saveTrainingData(trainingFile)
	if err != nil {
		log.Printf("[pipeline] 保存训练数据失败: %v", err)
		return
	}

	err = p.callTrainingService(trainingFile)
	if err != nil {
		log.Printf("[pipeline] 训练服务调用失败: %v", err)
		return
	}

	log.Println("[pipeline] 模型训练完成")
	p.lastTraining = time.Now()

	p.trainingMu.Lock()
	if len(p.trainingData) > 1000 {
		p.trainingData = p.trainingData[len(p.trainingData)-1000:]
	} else {
		p.trainingData = nil
	}
	p.trainingMu.Unlock()
}

func (p *Pipeline) saveTrainingData(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{"TranID", "Time", "Price", "Volume", "SaleOrderVolume", "BuyOrderVolume", "Type", "SaleOrderID", "SaleOrderPrice", "BuyOrderID", "BuyOrderPrice"})
	if err != nil {
		return err
	}

	p.trainingMu.Lock()
	data := p.trainingData
	p.trainingMu.Unlock()

	for _, tick := range data {
		record := []string{
			strconv.FormatInt(tick.TranID, 10),
			tick.Time,
			fmt.Sprintf("%.2f", tick.Price),
			strconv.FormatInt(tick.Volume, 10),
			strconv.FormatInt(tick.SaleOrderVolume, 10),
			strconv.FormatInt(tick.BuyOrderVolume, 10),
			tick.Type,
			strconv.FormatInt(tick.SaleOrderID, 10),
			fmt.Sprintf("%.2f", tick.SaleOrderPrice),
			strconv.FormatInt(tick.BuyOrderID, 10),
			fmt.Sprintf("%.2f", tick.BuyOrderPrice),
		}
		err = writer.Write(record)
		if err != nil {
			return err
		}
	}

	log.Printf("[pipeline] 训练数据已保存到: %s, 共 %d 条记录", filename, len(data))
	return nil
}

func (p *Pipeline) callTrainingService(dataFile string) error {
	request := map[string]interface{}{
		"data_path": dataFile,
		"model_config": map[string]interface{}{
			"epochs":        100,
			"batch_size":    32,
			"learning_rate": 0.001,
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return err
	}

	resp, err := http.Post(p.cfg.ML.ServiceURL+"/train", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("训练服务返回错误: %s, %s", resp.Status, string(body))
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}

	accuracy, _ := result["accuracy"].(float64)
	recall, _ := result["recall"].(float64)
	log.Printf("[pipeline] 训练完成, 准确率: %.2f, 召回率: %.2f", accuracy, recall)
	return nil
}

// AddTick 外部接口，接收tick数据
func (p *Pipeline) AddTick(tick *model.TickData) {
	select {
	case p.dataChan <- tick:
	default:
		log.Println("[pipeline] 数据通道已满，丢弃tick")
	}
}

// LoadCSVFromFile 从CSV文件加载历史数据
func (p *Pipeline) LoadCSVFromFile(filename string) error {
	log.Printf("[pipeline] 开始加载CSV文件: %s", filename)

	ticks, err := loader.LoadTickCSV(filename)
	if err != nil {
		return err
	}

	log.Printf("[pipeline] 加载完成，共 %d 条tick数据", len(ticks))

	for i := range ticks {
		p.AddTick(&ticks[i])
	}

	return nil
}

// RunFullPipeline 运行完整闭环（使用 engine.Pipeline）
func (p *Pipeline) RunFullPipeline() (*engine.RunResult, error) {
	engPipeline := engine.NewPipeline(engine.PipelineConfig{
		Symbol:       p.cfg.Pipeline.Symbol,
		TickDir:      p.cfg.Data.TickDir,
		OutputDir:    p.cfg.Data.OutputDir,
		ModelDir:     p.cfg.ML.ModelDir,
		ScriptDir:    p.cfg.ML.ScriptDir,
		PythonPath:   p.cfg.ML.PythonPath,
		ServiceURL:   p.cfg.ML.ServiceURL,
		TrainRatio:   p.cfg.Pipeline.TrainRatio,
		InitialCash:  p.cfg.Backtest.InitialCash,
		Commission:   p.cfg.Backtest.Commission,
		Slippage:     p.cfg.Backtest.Slippage,
		MaxPosition:  p.cfg.Backtest.MaxPosition,
		BarInterval:  p.cfg.Feature.BarInterval,
		WindowSize:   p.cfg.Feature.WindowSize,
		FutureSteps:  p.cfg.Feature.FutureSteps,
		PriceThresh:  p.cfg.Feature.PriceThresh,
		RetrainAfter: p.cfg.Pipeline.RetrainAfter,
		FeatureDim:   p.cfg.Pipeline.FeatureDim,
	})

	result := engPipeline.Run()
	if result.Error != "" {
		return &result, fmt.Errorf("%s", result.Error)
	}
	return &result, nil
}

// unused imports guard
var (
	_ = analysis.NewTrendAnalyzer
	_ = backtest.NewEngine
)

func main() {
	configPath := flag.String("config", "config.json", "config file path")
	csvFile := flag.String("csv", "", "CSV tick data file to process")
	fullPipeline := flag.Bool("full", false, "run full closed-loop pipeline (train+infer+retrain)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[main] 启动闭环处理流水线")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[main] 加载配置失败: %v", err)
	}

	os.MkdirAll(cfg.Data.OutputDir, 0755)
	os.MkdirAll(cfg.ML.ModelDir, 0755)

	pipeline := NewPipeline(cfg)

	if *fullPipeline {
		// 运行完整闭环
		result, err := pipeline.RunFullPipeline()
		if err != nil {
			log.Fatalf("[main] 闭环流水线失败: %v", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	err = pipeline.Start()
	if err != nil {
		log.Fatalf("[main] 启动流水线失败: %v", err)
	}

	if *csvFile != "" {
		err = pipeline.LoadCSVFromFile(*csvFile)
		if err != nil {
			log.Fatalf("[main] 处理CSV文件失败: %v", err)
		}
	}

	<-pipeline.stopChan
	log.Println("[main] 服务已停止")
}