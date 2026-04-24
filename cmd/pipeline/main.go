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
	"syscall"
	"time"

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
	featureEng        *feature.Engine
	backtest          *backtest.Engine
	dataChan          chan *model.Tick
	stopChan          chan struct{}
	trainingData      []*model.Tick
	lastTraining      time.Time
	trainingThreshold int
}

func NewPipeline(cfg *config.Config) *Pipeline {
	return &Pipeline{
		cfg:               cfg,
		signalEng:         engine.NewSignalEngine(cfg),
		featureEng:        feature.NewEngine(cfg),
		backtest:          backtest.NewEngine(cfg),
		dataChan:          make(chan *model.Tick, 1000),
		stopChan:          make(chan struct{}),
		trainingThreshold: cfg.Model.TrainingThreshold,
	}
}

func (p *Pipeline) Start() error {
	log.Println("[pipeline] 启动闭环处理服务")

	// 启动信号处理
	go p.handleSignals()

	// 启动数据处理循环
	go p.processLoop()

	// 启动定时训练检查
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

func (p *Pipeline) processTick(tick *model.Tick) {
	// 判断是历史数据还是实时数据
	now := time.Now()
	tickTime := time.Unix(0, tick.Timestamp*int64(time.Millisecond))
	isHistorical := tickTime.Before(now.Add(-time.Hour * 24)) // 超过24小时视为历史数据

	if isHistorical {
		// 历史数据加入训练集
		p.trainingData = append(p.trainingData, tick)
		log.Printf("[pipeline] 历史tick已加入训练集: %s, 总样本量: %d",
			tickTime.Format("2006-01-02 15:04:05"), len(p.trainingData))

		// 检查是否需要训练
		if len(p.trainingData) >= p.trainingThreshold {
			go p.startTraining()
		}
	} else {
		// 实时数据进行推理和交易
		log.Printf("[pipeline] 处理实时tick: %s, 价格: %.2f, 成交量: %d",
			tickTime.Format("2006-01-02 15:04:05"), tick.Price, tick.Volume)

		// 喂给信号引擎
		signal, err := p.signalEng.ProcessTick(tick)
		if err != nil {
			log.Printf("[pipeline] 信号处理错误: %v", err)
			return
		}

		if signal != nil {
			log.Printf("[pipeline] 生成交易信号: 方向=%s, 置信度=%.2f, 目标价格=%.2f",
				signal.Direction, signal.Confidence, signal.TargetPrice)

			// 执行交易
			trade, err := p.signalEng.ExecuteTrade(signal)
			if err != nil {
				log.Printf("[pipeline] 交易执行错误: %v", err)
				return
			}

			if trade != nil {
				log.Printf("[pipeline] 交易执行成功: ID=%s, 数量=%.4f, 价格=%.2f",
					trade.ID, trade.Quantity, trade.ExecPrice)

				// 记录到回测引擎
				p.backtest.RecordTrade(trade)

				// 生成报告
				go p.generateReport()
			}
		}

		// 也加入训练数据
		p.trainingData = append(p.trainingData, tick)
	}
}

func (p *Pipeline) trainingScheduler() {
	ticker := time.NewTicker(24 * time.Hour) // 每天检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if len(p.trainingData) > 0 && time.Since(p.lastTraining) > 7*24*time.Hour {
				go p.startTraining()
			}
		case <-p.stopChan:
			return
		}
	}
}

func (p *Pipeline) startTraining() {
	log.Println("[pipeline] 开始模型训练...")

	// 保存训练数据
	trainingFile := filepath.Join(p.cfg.Data.DataDir, fmt.Sprintf("training_data_%s.csv",
		time.Now().Format("20060102_150405")))

	err := p.saveTrainingData(trainingFile)
	if err != nil {
		log.Printf("[pipeline] 保存训练数据失败: %v", err)
		return
	}

	// 调用ML服务训练
	err = p.callTrainingService(trainingFile)
	if err != nil {
		log.Printf("[pipeline] 训练服务调用失败: %v", err)
		return
	}

	log.Println("[pipeline] 模型训练完成")
	p.lastTraining = time.Now()

	// 清空训练数据（保留最近10%作为验证集）
	if len(p.trainingData) > 1000 {
		p.trainingData = p.trainingData[len(p.trainingData)-1000:]
	} else {
		p.trainingData = nil
	}
}

func (p *Pipeline) saveTrainingData(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写表头
	err = writer.Write([]string{"TranID", "Time", "Price", "Volume", "SaleOrderVolume", "BuyOrderVolume", "Type", "SaleOrderID", "SaleOrderPrice", "BuyOrderID", "BuyOrderPrice"})
	if err != nil {
		return err
	}

	// 写数据
	for _, tick := range p.trainingData {
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

	log.Printf("[pipeline] 训练数据已保存到: %s, 共 %d 条记录", filename, len(p.trainingData))
	return nil
}

func (p *Pipeline) callTrainingService(dataFile string) error {
	// 构造训练请求
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

	log.Printf("[pipeline] 训练完成, 准确率: %.2f, 召回率: %.2f",
		result["accuracy"].(float64), result["recall"].(float64))
	return nil
}

func (p *Pipeline) generateReport() {
	// 计算绩效指标
	metrics := p.backtest.CalculateMetrics()

	// 生成报告
	report := backtest.GenerateReport(metrics, p.backtest.GetTrades())

	// 保存报告
	reportFile := filepath.Join(p.cfg.Data.ReportDir, fmt.Sprintf("trade_report_%s.md",
		time.Now().Format("20060102_150405")))

	err := os.WriteFile(reportFile, []byte(report), 0644)
	if err != nil {
		log.Printf("[pipeline] 保存报告失败: %v", err)
		return
	}

	log.Printf("[pipeline] 交易报告已生成: %s", reportFile)
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

	ticks, err := loader.LoadCSV(filename)
	if err != nil {
		return err
	}

	log.Printf("[pipeline] 加载完成，共 %d 条tick数据", len(ticks))

	// 送入处理通道
	for _, tick := range ticks {
		p.AddTick(tick)
	}

	return nil
}

func main() {
	configPath := flag.String("config", "config.json", "config file path")
	csvFile := flag.String("csv", "", "CSV tick data file to process")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[main] 启动闭环处理流水线")

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[main] 加载配置失败: %v", err)
	}

	// 创建数据目录
	os.MkdirAll(cfg.Data.OutputDir, 0755)
	os.MkdirAll(cfg.ML.ModelDir, 0755)
	reportDir := filepath.Join(cfg.Data.OutputDir, "reports")
	os.MkdirAll(reportDir, 0755)

	// 初始化流水线
	pipeline := NewPipeline(cfg)

	// 启动服务
	err = pipeline.Start()
	if err != nil {
		log.Fatalf("[main] 启动流水线失败: %v", err)
	}

	// 如果指定了CSV文件，先处理
	if *csvFile != "" {
		err = pipeline.LoadCSVFromFile(*csvFile)
		if err != nil {
			log.Fatalf("[main] 处理CSV文件失败: %v", err)
		}
	}

	// 等待停止
	<-pipeline.stopChan
	log.Println("[main] 服务已停止")
}
