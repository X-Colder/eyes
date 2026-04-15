package feature

import (
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/eyes/internal/model"
)

// Engineer 特征工程器：将 tick 数据转换为模型可用的特征
type Engineer struct {
	BarInterval int     // 聚合秒数
	WindowSize  int     // 滑动窗口（用多少根bar生成一条特征）
	FutureSteps int     // 预测未来N根bar
	PriceThresh float64 // 涨跌标签阈值（百分比）
}

// NewEngineer 创建特征工程器
func NewEngineer(barInterval, windowSize, futureSteps int, priceThresh float64) *Engineer {
	return &Engineer{
		BarInterval: barInterval,
		WindowSize:  windowSize,
		FutureSteps: futureSteps,
		PriceThresh: priceThresh,
	}
}

// AggregateBars 将 tick 数据聚合为固定时间间隔的 bar
func (e *Engineer) AggregateBars(ticks []model.TickData) []model.TickBar {
	if len(ticks) == 0 {
		return nil
	}

	var bars []model.TickBar
	var currentBar *model.TickBar
	currentSlot := ""

	for _, tick := range ticks {
		slot := e.timeSlot(tick.Time)
		if slot != currentSlot {
			if currentBar != nil {
				e.finalizeBar(currentBar)
				bars = append(bars, *currentBar)
			}
			currentBar = &model.TickBar{
				StartTime: tick.Time,
				Open:      tick.Price,
				High:      tick.Price,
				Low:       tick.Price,
				Close:     tick.Price,
			}
			currentSlot = slot
		}

		// 更新当前 bar
		currentBar.EndTime = tick.Time
		currentBar.Close = tick.Price
		if tick.Price > currentBar.High {
			currentBar.High = tick.Price
		}
		if tick.Price < currentBar.Low {
			currentBar.Low = tick.Price
		}
		currentBar.Volume += tick.Volume
		currentBar.Amount += tick.Price * float64(tick.Volume)
		currentBar.TradeCount++

		if tick.Type == "B" {
			currentBar.BuyVolume += tick.Volume
		} else {
			currentBar.SellVolume += tick.Volume
		}
	}

	// 最后一根 bar
	if currentBar != nil {
		e.finalizeBar(currentBar)
		bars = append(bars, *currentBar)
	}

	log.Printf("[feature] aggregated %d ticks into %d bars (interval=%ds)", len(ticks), len(bars), e.BarInterval)
	return bars
}

// timeSlot 计算 tick 所属的时间槽
func (e *Engineer) timeSlot(timeStr string) string {
	parts := strings.Split(timeStr, ":")
	if len(parts) < 3 {
		return timeStr
	}
	h, m, s := 0, 0, 0
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	fmt.Sscanf(parts[2], "%d", &s)

	totalSec := h*3600 + m*60 + s
	slotStart := (totalSec / e.BarInterval) * e.BarInterval

	sh := slotStart / 3600
	sm := (slotStart % 3600) / 60
	ss := slotStart % 60
	return fmt.Sprintf("%02d:%02d:%02d", sh, sm, ss)
}

func (e *Engineer) finalizeBar(bar *model.TickBar) {
	if bar.Volume > 0 {
		bar.VWAP = bar.Amount / float64(bar.Volume)
	}
}

// ExtractFeatures 从 bar 序列提取特征向量 + 标签
func (e *Engineer) ExtractFeatures(bars []model.TickBar) []model.Feature {
	if len(bars) < e.WindowSize+e.FutureSteps {
		log.Printf("[feature] not enough bars: %d < window(%d)+future(%d)",
			len(bars), e.WindowSize, e.FutureSteps)
		return nil
	}

	var features []model.Feature
	for i := e.WindowSize - 1; i < len(bars)-e.FutureSteps; i++ {
		window := bars[i-e.WindowSize+1 : i+1]
		futureBar := bars[i+e.FutureSteps]

		vals := e.computeFeatureVector(window)
		currentPrice := window[len(window)-1].Close
		futurePrice := futureBar.Close
		priceChg := 0.0
		if currentPrice > 0 {
			priceChg = (futurePrice - currentPrice) / currentPrice * 100
		}

		label := 0 // 持平/观望
		if priceChg > e.PriceThresh {
			label = 1 // 涨
		} else if priceChg < -e.PriceThresh {
			label = 0 // 跌
		}

		features = append(features, model.Feature{
			Time:     window[len(window)-1].EndTime,
			Values:   vals,
			Label:    label,
			PriceChg: priceChg,
		})
	}

	log.Printf("[feature] extracted %d feature vectors (dim=%d)", len(features), len(features[0].Values))
	return features
}

// ExtractFeaturesWithMeta 从 bar 序列提取特征向量并附加 date/symbol 元信息
func (e *Engineer) ExtractFeaturesWithMeta(bars []model.TickBar, date, symbol string) []model.Feature {
	features := e.ExtractFeatures(bars)
	for i := range features {
		features[i].Date = date
		features[i].Symbol = symbol
	}
	return features
}

// computeFeatureVector 从一个窗口的 bar 数据计算特征向量
// 特征设计（每根bar 14个特征 × windowSize + 10个窗口统计 = 总特征维度）
func (e *Engineer) computeFeatureVector(window []model.TickBar) []float64 {
	var vals []float64

	// 1) 每根 bar 的逐根特征
	for _, bar := range window {
		// 价格相关 (4)
		vals = append(vals, bar.Open, bar.High, bar.Low, bar.Close)
		// 量价关系 (4)
		vals = append(vals, float64(bar.Volume), bar.Amount, bar.VWAP, float64(bar.TradeCount))
		// 买卖力度 (3)
		totalVol := float64(bar.BuyVolume + bar.SellVolume)
		buyRatio := 0.0
		if totalVol > 0 {
			buyRatio = float64(bar.BuyVolume) / totalVol
		}
		vals = append(vals, float64(bar.BuyVolume), float64(bar.SellVolume), buyRatio)
		// K线形态 (3)
		body := bar.Close - bar.Open                            // 实体
		upperShadow := bar.High - math.Max(bar.Open, bar.Close) // 上影线
		lowerShadow := math.Min(bar.Open, bar.Close) - bar.Low  // 下影线
		vals = append(vals, body, upperShadow, lowerShadow)
	}

	// 2) 窗口级统计特征
	n := len(window)
	closes := make([]float64, n)
	volumes := make([]float64, n)
	for i, bar := range window {
		closes[i] = bar.Close
		volumes[i] = float64(bar.Volume)
	}

	// 价格变化率
	firstClose := closes[0]
	lastClose := closes[n-1]
	priceReturn := 0.0
	if firstClose > 0 {
		priceReturn = (lastClose - firstClose) / firstClose * 100
	}
	vals = append(vals, priceReturn)

	// 价格波动率
	vals = append(vals, stddev(closes))

	// 价格动量（最近3根 vs 之前）
	mid := n / 2
	recentAvg := mean(closes[mid:])
	earlyAvg := mean(closes[:mid])
	momentum := 0.0
	if earlyAvg > 0 {
		momentum = (recentAvg - earlyAvg) / earlyAvg * 100
	}
	vals = append(vals, momentum)

	// 成交量趋势
	recentVolAvg := mean(volumes[mid:])
	earlyVolAvg := mean(volumes[:mid])
	volTrend := 0.0
	if earlyVolAvg > 0 {
		volTrend = (recentVolAvg - earlyVolAvg) / earlyVolAvg * 100
	}
	vals = append(vals, volTrend)

	// 总量统计
	vals = append(vals, sum(volumes))
	vals = append(vals, mean(volumes))

	// 最高最低价
	maxPrice, minPrice := closes[0], closes[0]
	for _, c := range closes {
		if c > maxPrice {
			maxPrice = c
		}
		if c < minPrice {
			minPrice = c
		}
	}
	vals = append(vals, maxPrice, minPrice)

	// 买入比率窗口均值
	buyRatios := make([]float64, n)
	for i, bar := range window {
		total := float64(bar.BuyVolume + bar.SellVolume)
		if total > 0 {
			buyRatios[i] = float64(bar.BuyVolume) / total
		}
	}
	vals = append(vals, mean(buyRatios))

	// VWAP 趋势
	vwaps := make([]float64, n)
	for i, bar := range window {
		vwaps[i] = bar.VWAP
	}
	vals = append(vals, stddev(vwaps))

	return vals
}

// --- 数学工具 ---

func mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range data {
		s += v
	}
	return s / float64(len(data))
}

func sum(data []float64) float64 {
	s := 0.0
	for _, v := range data {
		s += v
	}
	return s
}

func stddev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	m := mean(data)
	ss := 0.0
	for _, v := range data {
		d := v - m
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(data)-1))
}
