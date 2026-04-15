package analysis

import (
	"log"
	"math"

	"github.com/eyes/internal/model"
)

// TrendAnalyzer 趋势分析器：识别 tick/bar 数据中的涨跌阶段
type TrendAnalyzer struct {
	SmoothWindow int     // 平滑窗口大小
	PeakThresh   float64 // 极值判定阈值（%）
}

// NewTrendAnalyzer 创建趋势分析器
func NewTrendAnalyzer(smoothWindow int, peakThresh float64) *TrendAnalyzer {
	return &TrendAnalyzer{
		SmoothWindow: smoothWindow,
		PeakThresh:   peakThresh,
	}
}

// IdentifyPhases 识别每根 bar 所属的趋势阶段
func (ta *TrendAnalyzer) IdentifyPhases(bars []model.TickBar) []model.TrendPhase {
	n := len(bars)
	if n == 0 {
		return nil
	}

	// 1) 提取收盘价序列并平滑
	closes := make([]float64, n)
	for i, bar := range bars {
		closes[i] = bar.Close
	}
	smoothed := ta.movingAverage(closes, ta.SmoothWindow)

	// 2) 寻找局部极值点
	peaks, troughs := ta.findExtremes(smoothed)
	log.Printf("[analysis] found %d peaks, %d troughs in %d bars", len(peaks), len(troughs), n)

	// 3) 为每根 bar 标注趋势阶段
	phases := make([]model.TrendPhase, n)
	for i := range phases {
		phases[i] = model.TrendUnknown
	}

	// 标记极值点附近区域
	peakRadius := max(ta.SmoothWindow/2, 3)
	for _, idx := range peaks {
		for j := max(0, idx-peakRadius); j <= min(n-1, idx+peakRadius); j++ {
			phases[j] = model.TrendPeak
		}
	}
	for _, idx := range troughs {
		for j := max(0, idx-peakRadius); j <= min(n-1, idx+peakRadius); j++ {
			phases[j] = model.TrendTrough
		}
	}

	// 填充上涨/下跌区间
	for i := 1; i < n; i++ {
		if phases[i] == model.TrendUnknown {
			if smoothed[i] > smoothed[i-1] {
				phases[i] = model.TrendRising
			} else if smoothed[i] < smoothed[i-1] {
				phases[i] = model.TrendFalling
			} else if i > 0 {
				phases[i] = phases[i-1] // 沿用前一根
			}
		}
	}

	return phases
}

// LabelFeatures 为特征数据添加趋势阶段标注
func (ta *TrendAnalyzer) LabelFeatures(features []model.Feature, bars []model.TickBar) []model.Feature {
	phases := ta.IdentifyPhases(bars)
	if len(phases) == 0 {
		return features
	}

	// 构建时间到阶段的映射
	timeToPhase := make(map[string]model.TrendPhase)
	for i, bar := range bars {
		if i < len(phases) {
			timeToPhase[bar.EndTime] = phases[i]
		}
	}

	// 标注特征
	for i := range features {
		if phase, ok := timeToPhase[features[i].Time]; ok {
			features[i].Phase = phase
		}
	}

	// 统计各阶段分布
	phaseCounts := make(map[model.TrendPhase]int)
	for _, f := range features {
		phaseCounts[f.Phase]++
	}
	log.Printf("[analysis] feature phase distribution: rising=%d peak=%d falling=%d trough=%d unknown=%d",
		phaseCounts[model.TrendRising], phaseCounts[model.TrendPeak],
		phaseCounts[model.TrendFalling], phaseCounts[model.TrendTrough],
		phaseCounts[model.TrendUnknown])

	return features
}

// GenerateTradeLabels 生成交易标签（在高点前卖出、低点前买入的理想策略）
func (ta *TrendAnalyzer) GenerateTradeLabels(bars []model.TickBar) []model.TradeLabel {
	phases := ta.IdentifyPhases(bars)
	labels := make([]model.TradeLabel, len(bars))

	for i, bar := range bars {
		labels[i] = model.TradeLabel{
			Time:  bar.EndTime,
			Phase: phases[i],
		}

		// 计算未来价格变化
		if i < len(bars)-1 {
			futurePrice := bars[min(i+5, len(bars)-1)].Close
			priceChg := 0.0
			if bar.Close > 0 {
				priceChg = (futurePrice - bar.Close) / bar.Close * 100
			}
			labels[i].PriceChg = priceChg
			if priceChg > 0 {
				labels[i].PriceDir = 1
			} else {
				labels[i].PriceDir = -1
			}
		}

		// 根据趋势阶段确定理想动作
		switch phases[i] {
		case model.TrendTrough:
			labels[i].Action = 1 // 买入
			labels[i].Confidence = 0.8
		case model.TrendRising:
			labels[i].Action = 0 // 持仓
			labels[i].Confidence = 0.6
		case model.TrendPeak:
			labels[i].Action = 2 // 卖出
			labels[i].Confidence = 0.8
		case model.TrendFalling:
			labels[i].Action = 0 // 观望
			labels[i].Confidence = 0.6
		}
	}

	return labels
}

// --- 内部工具函数 ---

func (ta *TrendAnalyzer) movingAverage(data []float64, window int) []float64 {
	n := len(data)
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		start := max(0, i-window/2)
		end := min(n, i+window/2+1)
		sum := 0.0
		for j := start; j < end; j++ {
			sum += data[j]
		}
		result[i] = sum / float64(end-start)
	}
	return result
}

func (ta *TrendAnalyzer) findExtremes(data []float64) (peaks, troughs []int) {
	n := len(data)
	if n < 3 {
		return
	}
	for i := 1; i < n-1; i++ {
		if data[i] > data[i-1] && data[i] > data[i+1] {
			// 检查幅度是否超阈值
			leftChg := math.Abs((data[i] - data[i-1]) / data[i-1] * 100)
			rightChg := math.Abs((data[i] - data[i+1]) / data[i] * 100)
			if leftChg >= ta.PeakThresh || rightChg >= ta.PeakThresh {
				peaks = append(peaks, i)
			}
		}
		if data[i] < data[i-1] && data[i] < data[i+1] {
			leftChg := math.Abs((data[i-1] - data[i]) / data[i-1] * 100)
			rightChg := math.Abs((data[i+1] - data[i]) / data[i] * 100)
			if leftChg >= ta.PeakThresh || rightChg >= ta.PeakThresh {
				troughs = append(troughs, i)
			}
		}
	}
	return
}
