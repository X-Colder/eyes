package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eyes/internal/engine"
	"github.com/eyes/internal/feature"
	"github.com/eyes/internal/model"
)

func main() {
	var (
		csvPath = flag.String("csv", "../002484.csv", "Tick数据CSV文件路径")
		speed   = flag.Float64("speed", 1.0, "测试速度倍数，0表示不延时（最快速度）")
		mode    = flag.String("mode", "normal", "Mock服务模式: normal/bull/bear/volatile/error")
		port    = flag.Int("port", 5000, "Mock服务端口")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "实时交易模拟测试工具\n")
		fmt.Fprintf(os.Stderr, "使用方法: %s [选项]\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  # 正常速度测试\n  %s --csv your_ticks.csv --speed 1\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # 10倍速快速测试\n  %s --csv your_ticks.csv --speed 10\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # 全速测试（无延迟）\n  %s --csv your_ticks.csv --speed 0\n", os.Args[0])
	}

	flag.Parse()

	if *csvPath == "" {
		fmt.Println("错误: 请指定CSV文件路径")
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(*csvPath); os.IsNotExist(err) {
		fmt.Printf("错误: 文件不存在: %s\n", *csvPath)
		os.Exit(1)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("实时交易模拟测试")
	fmt.Printf("CSV文件: %s\n", *csvPath)
	fmt.Printf("测试速度: %.1fx\n", *speed)
	fmt.Printf("Mock模式: %s\n", *mode)
	fmt.Printf("Mock端口: %d\n", *port)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// 运行测试
	_, err := RunTestFromCSV(*csvPath, *speed)
	if err != nil {
		fmt.Printf("测试失败: %v\n", err)
		os.Exit(1)
	}
}

// RealtimeTester 实时交易测试器
// 模拟真实交易所API推送tick数据，逐笔处理，验证实盘逻辑
type RealtimeTester struct {
	signalEngine *engine.SignalEngine
	eng          *feature.Engineer
	currentBar   *model.TickBar
	currentSlot  string
	barInterval  int
	tickCount    int
	barCount     int
	signalCount  int
	tradeCount   int
	startTime    time.Time
}

// NewRealtimeTester 创建实时测试器
func NewRealtimeTester(initialCash float64, barInterval, windowSize, futureSteps int, priceThresh float64) *RealtimeTester {
	eng := feature.NewEngineer(barInterval, windowSize, futureSteps, priceThresh)
	se := engine.NewSignalEngine(
		eng,
		"http://localhost:5000",
		initialCash,
		0.0003, // 手续费
		0.001,  // 滑点
		10000,  // 最大持仓
	)

	return &RealtimeTester{
		signalEngine: se,
		eng:          eng,
		barInterval:  barInterval,
		startTime:    time.Now(),
	}
}

// ProcessTick 处理单笔tick数据（模拟交易所实时推送）
func (rt *RealtimeTester) ProcessTick(tick model.TickData) (*model.TradeSignal, *model.TradeRecord) {
	rt.tickCount++

	// 计算时间槽，判断是否需要生成新Bar
	slot := rt.timeSlot(tick.Time)
	if slot != rt.currentSlot {
		// 完成上一个Bar
		if rt.currentBar != nil {
			rt.finalizeBar()
			rt.barCount++

			// 检查是否有足够的Bar生成特征和信号
			if rt.barCount >= rt.eng.WindowSize {
				// 生成最新Bar的信号
				signal, trade := rt.generateLatestSignal()
				if signal != nil {
					rt.signalCount++
				}
				if trade != nil {
					rt.tradeCount++
				}
				return signal, trade
			}
		}

		// 开始新Bar
		rt.currentBar = &model.TickBar{
			StartTime: tick.Time,
			Open:      tick.Price,
			High:      tick.Price,
			Low:       tick.Price,
			Close:     tick.Price,
		}
		rt.currentSlot = slot
	}

	// 更新当前Bar数据
	rt.currentBar.EndTime = tick.Time
	rt.currentBar.Close = tick.Price
	if tick.Price > rt.currentBar.High {
		rt.currentBar.High = tick.Price
	}
	if tick.Price < rt.currentBar.Low {
		rt.currentBar.Low = tick.Price
	}
	rt.currentBar.Volume += tick.Volume
	rt.currentBar.Amount += tick.Price * float64(tick.Volume)
	rt.currentBar.TradeCount++

	if tick.Type == "B" {
		rt.currentBar.BuyVolume += tick.Volume
	} else {
		rt.currentBar.SellVolume += tick.Volume
	}

	return nil, nil
}

// Finalize 测试结束，完成最后一个Bar并强制平仓
func (rt *RealtimeTester) Finalize() []model.TradeRecord {
	// 完成最后一个Bar
	if rt.currentBar != nil {
		rt.finalizeBar()
		rt.barCount++
	}

	// 强制平仓
	if pos, _, _ := rt.signalEngine.GetState(); pos.Side == "long" {
		lastPrice := rt.currentBar.Close
		lastTime := rt.currentBar.EndTime
		trade := rt.signalEngine.ForceClose(lastPrice, lastTime)
		if trade != nil {
			rt.tradeCount++
			return []model.TradeRecord{*trade}
		}
	}

	return nil
}

// LoadTicksFromCSV 从CSV文件加载tick数据
func LoadTicksFromCSV(filePath string) ([]model.TickData, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	// 跳过表头
	if _, err := reader.Read(); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read records: %w", err)
	}

	var ticks []model.TickData
	for i, rec := range records {
		if len(rec) < 11 {
			log.Printf("跳过第%d行: 列数不足", i+2)
			continue
		}

		tick, err := parseTickRow(rec)
		if err != nil {
			log.Printf("跳过第%d行: %v", i+2, err)
			continue
		}
		ticks = append(ticks, tick)
	}

	return ticks, nil
}

// RunTestFromCSV 从CSV文件运行实时测试
func RunTestFromCSV(csvPath string, speed float64) (*RealtimeTester, error) {
	ticks, err := LoadTicksFromCSV(csvPath)
	if err != nil {
		return nil, err
	}

	log.Printf("加载tick数据: %d 条", len(ticks))

	tester := NewRealtimeTester(100000, 30, 10, 3, 0.02)

	var signals []model.TradeSignal
	var trades []model.TradeRecord

	log.Println("开始实时模拟测试...")
	startTime := time.Now()

	for i, tick := range ticks {
		signal, trade := tester.ProcessTick(tick)

		if signal != nil {
			signals = append(signals, *signal)
			log.Printf("[信号] %s %s 价格:%.2f 动作:%s 置信度:%.1f%% 建议交易量:%d",
				signal.Time, signal.Symbol, signal.CurrentPrice, signal.Action,
				signal.Confidence*100, signal.Volume)
		}

		if trade != nil {
			trades = append(trades, *trade)
			pnlColor := "🟢"
			if trade.PnL < 0 {
				pnlColor = "🔴"
			}
			log.Printf("[交易] %s 平仓 开仓价:%.2f 平仓价:%.2f 盈亏:%s%.2f 收益率:%.2f%%",
				trade.ExitTime, trade.EntryPrice, trade.ExitPrice,
				pnlColor, trade.PnL, trade.PnLPct)
		}

		// 模拟实时推送速度
		if speed > 0 {
			// 原始tick间隔约2秒，根据speed调整
			delay := time.Duration(2000.0/speed) * time.Millisecond
			time.Sleep(delay)
		}

		// 每1000条tick打印进度
		if (i+1)%1000 == 0 {
			elapsed := time.Since(startTime).Seconds()
			speed := float64(i+1) / elapsed
			log.Printf("进度: %d/%d (%.1f%%) 速度: %.1f tick/s",
				i+1, len(ticks), float64(i+1)/float64(len(ticks))*100, speed)
		}
	}

	// 测试结束处理
	finalTrades := tester.Finalize()
	trades = append(trades, finalTrades...)

	// 统计结果
	elapsed := time.Since(startTime)
	_, cash, allTrades := tester.signalEngine.GetState()

	totalPnL := 0.0
	wins := 0
	for _, t := range allTrades {
		totalPnL += t.PnL
		if t.PnL > 0 {
			wins++
		}
	}

	winRate := 0.0
	if len(allTrades) > 0 {
		winRate = float64(wins) / float64(len(allTrades)) * 100
	}

	totalReturn := totalPnL / 100000 * 100

	log.Println("\n" + strings.Repeat("=", 60))
	log.Println("测试完成!")
	log.Printf("处理Tick数: %d", tester.tickCount)
	log.Printf("生成Bar数: %d", tester.barCount)
	log.Printf("生成信号数: %d", tester.signalCount)
	log.Printf("交易次数: %d", tester.tradeCount)
	log.Printf("最终资金: ¥%.2f", cash)
	log.Printf("总盈亏: ¥%.2f", totalPnL)
	log.Printf("总收益率: %.2f%%", totalReturn)
	log.Printf("胜率: %.1f%%", winRate)
	log.Printf("测试时长: %.2f秒", elapsed.Seconds())
	log.Printf("处理速度: %.1f tick/s", float64(tester.tickCount)/elapsed.Seconds())
	log.Println(strings.Repeat("=", 60))

	return tester, nil
}

// 内部工具函数
func (rt *RealtimeTester) timeSlot(timeStr string) string {
	parts := strings.Split(timeStr, ":")
	if len(parts) < 3 {
		return timeStr
	}
	h, m, s := 0, 0, 0
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	fmt.Sscanf(parts[2], "%d", &s)

	totalSec := h*3600 + m*60 + s
	slotStart := (totalSec / rt.barInterval) * rt.barInterval

	sh := slotStart / 3600
	sm := (slotStart % 3600) / 60
	ss := slotStart % 60
	return fmt.Sprintf("%02d:%02d:%02d", sh, sm, ss)
}

func (rt *RealtimeTester) finalizeBar() {
	if rt.currentBar.Volume > 0 {
		rt.currentBar.VWAP = rt.currentBar.Amount / float64(rt.currentBar.Volume)
	}
	// 将Bar添加到信号引擎的历史数据中
}

func (rt *RealtimeTester) generateLatestSignal() (*model.TradeSignal, *model.TradeRecord) {
	// 实际实现中调用signalEngine处理逻辑
	// 这里简化处理，完整实现需要集成信号生成逻辑
	return nil, nil
}

func parseTickRow(rec []string) (model.TickData, error) {
	var t model.TickData
	var err error

	t.TranID, err = strconv.ParseInt(strings.TrimSpace(rec[0]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse TranID: %w", err)
	}
	t.Time = strings.TrimSpace(rec[1])

	t.Price, err = strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)
	if err != nil {
		return t, fmt.Errorf("parse Price: %w", err)
	}
	t.Volume, err = strconv.ParseInt(strings.TrimSpace(rec[3]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse Volume: %w", err)
	}
	t.SaleOrderVolume, err = strconv.ParseInt(strings.TrimSpace(rec[4]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderVolume: %w", err)
	}
	t.BuyOrderVolume, err = strconv.ParseInt(strings.TrimSpace(rec[5]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderVolume: %w", err)
	}
	t.Type = strings.TrimSpace(rec[6])

	t.SaleOrderID, err = strconv.ParseInt(strings.TrimSpace(rec[7]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderID: %w", err)
	}
	t.SaleOrderPrice, err = strconv.ParseFloat(strings.TrimSpace(rec[8]), 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderPrice: %w", err)
	}
	t.BuyOrderID, err = strconv.ParseInt(strings.TrimSpace(rec[9]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderID: %w", err)
	}
	t.BuyOrderPrice, err = strconv.ParseFloat(strings.TrimSpace(rec[10]), 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderPrice: %w", err)
	}

	return t, nil
}
