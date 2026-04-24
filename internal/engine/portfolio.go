package engine

import (
	"math"
	"sync"

	"github.com/eyes/internal/model"
	"github.com/eyes/internal/plugin"
)

// PortfolioEngine 组合交易引擎
// 负责多品种信号处理、相关性分析和组合优化
type PortfolioEngine struct {
	registry    *plugin.PluginRegistry
	instruments map[string]*model.InstrumentMeta

	// 各品种独立引擎
	signalEngines map[string]*SignalEngine

	// 组合分析器
	correlationAnalyzer *CorrelationAnalyzer
	basisAnalyzer       *BasisAnalyzer

	// 组合优化器
	optimizer *PortfolioOptimizer

	// 运行时状态
	positions   map[string]*model.Position // symbol -> position
	signals     map[string][]model.TradeSignal
	predictions map[string]*model.PredictionResult

	mu sync.RWMutex
}

// NewPortfolioEngine 创建组合交易引擎
func NewPortfolioEngine(
	registry *plugin.PluginRegistry,
	optimizerConfig *OptimizerConfig,
) *PortfolioEngine {
	return &PortfolioEngine{
		registry:            registry,
		instruments:         make(map[string]*model.InstrumentMeta),
		signalEngines:       make(map[string]*SignalEngine),
		correlationAnalyzer: NewCorrelationAnalyzer(60), // 60个bar窗口
		basisAnalyzer:       NewBasisAnalyzer(),
		optimizer:           NewPortfolioOptimizer(optimizerConfig),
		positions:           make(map[string]*model.Position),
		signals:             make(map[string][]model.TradeSignal),
		predictions:         make(map[string]*model.PredictionResult),
	}
}

// RegisterInstrument 注册品种
func (pe *PortfolioEngine) RegisterInstrument(
	meta *model.InstrumentMeta,
	signalEngine *SignalEngine,
) {
	pe.instruments[meta.Symbol] = meta
	pe.signalEngines[meta.Symbol] = signalEngine
}

// ProcessMultiSymbolSignals 处理多品种信号并优化组合
func (pe *PortfolioEngine) ProcessMultiSymbolSignals(
	date string,
	ticksBySymbol map[string][]model.TickData,
) *OptimizedPortfolio {

	pe.mu.Lock()
	defer pe.mu.Unlock()

	// 1. 独立处理各品种信号
	var wg sync.WaitGroup
	var mu sync.Mutex

	for symbol, ticks := range ticksBySymbol {
		wg.Add(1)
		go func(sym string, tks []model.TickData) {
			defer wg.Done()

			engine, ok := pe.signalEngines[sym]
			if !ok {
				return
			}

			signals, _ := engine.ProcessDayTicks(date, sym, tks)

			mu.Lock()
			pe.signals[sym] = signals
			if len(signals) > 0 {
				pe.predictions[sym] = &model.PredictionResult{
					Symbol:     sym,
					Action:     signals[len(signals)-1].Action,
					Confidence: signals[len(signals)-1].Confidence,
				}
			}
			mu.Unlock()
		}(symbol, ticks)
	}

	wg.Wait()

	// 2. 相关性分析
	correlations := pe.analyzeCorrelations(ticksBySymbol)

	// 3. 基差分析 (期货与现货)
	basisMap := pe.analyzeBasis(ticksBySymbol)

	// 4. 组合优化
	optimized := pe.optimizer.Optimize(
		pe.signals,
		pe.predictions,
		pe.positions,
		correlations,
		basisMap,
		pe.instruments,
	)

	return optimized
}

// analyzeCorrelations 分析品种间相关性
func (pe *PortfolioEngine) analyzeCorrelations(
	ticksBySymbol map[string][]model.TickData,
) map[string]map[string]float64 {

	correlations := make(map[string]map[string]float64)
	symbols := make([]string, 0, len(ticksBySymbol))

	for sym := range ticksBySymbol {
		symbols = append(symbols, sym)
	}

	// 两两计算相关系数
	for i, sym1 := range symbols {
		correlations[sym1] = make(map[string]float64)
		for j, sym2 := range symbols {
			if i >= j {
				continue
			}

			// 计算相关系数
			corr := pe.correlationAnalyzer.CalcCorrelation(
				ticksBySymbol[sym1],
				ticksBySymbol[sym2],
			)

			correlations[sym1][sym2] = corr
			if correlations[sym2] == nil {
				correlations[sym2] = make(map[string]float64)
			}
			correlations[sym2][sym1] = corr
		}
		correlations[sym1][sym1] = 1.0
	}

	return correlations
}

// analyzeBasis 分析基差 (期货与现货价差)
func (pe *PortfolioEngine) analyzeBasis(
	ticksBySymbol map[string][]model.TickData,
) map[string]float64 {

	basisMap := make(map[string]float64)

	// 找出期货与对应现货的关系
	for symbol, ticks := range ticksBySymbol {
		meta, ok := pe.instruments[symbol]
		if !ok {
			continue
		}

		// 如果是期货,找对应的现货
		if meta.Type == model.TypeFuture && meta.UnderlyingSymbol != "" {
			spotTicks, ok := ticksBySymbol[meta.UnderlyingSymbol]
			if ok && len(ticks) > 0 && len(spotTicks) > 0 {
				futuresPrice := ticks[len(ticks)-1].Price
				spotPrice := spotTicks[len(spotTicks)-1].Price

				basis := pe.basisAnalyzer.CalcBasis(futuresPrice, spotPrice)
				basisMap[symbol] = basis
			}
		}
	}

	return basisMap
}

// GetPositions 获取所有持仓
func (pe *PortfolioEngine) GetPositions() map[string]*model.Position {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	result := make(map[string]*model.Position)
	for k, v := range pe.positions {
		result[k] = v
	}
	return result
}

// CorrelationAnalyzer 相关性分析器
type CorrelationAnalyzer struct {
	windowSize int
}

// NewCorrelationAnalyzer 创建相关性分析器
func NewCorrelationAnalyzer(windowSize int) *CorrelationAnalyzer {
	return &CorrelationAnalyzer{windowSize: windowSize}
}

// CalcCorrelation 计算两个品种的相关系数
func (ca *CorrelationAnalyzer) CalcCorrelation(
	ticks1, ticks2 []model.TickData,
) float64 {

	n := min(len(ticks1), len(ticks2))
	if n < 10 {
		return 0
	}

	// 提取价格序列
	prices1 := make([]float64, n)
	prices2 := make([]float64, n)

	for i := 0; i < n; i++ {
		prices1[i] = ticks1[i].Price
		prices2[i] = ticks2[i].Price
	}

	// 计算收益率
	returns1 := ca.calcReturns(prices1)
	returns2 := ca.calcReturns(prices2)

	// 计算Pearson相关系数
	return ca.pearsonCorrelation(returns1, returns2)
}

// calcReturns 计算收益率序列
func (ca *CorrelationAnalyzer) calcReturns(prices []float64) []float64 {
	n := len(prices)
	if n < 2 {
		return nil
	}

	returns := make([]float64, n-1)
	for i := 1; i < n; i++ {
		if prices[i-1] > 0 {
			returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
		}
	}
	return returns
}

// pearsonCorrelation 计算Pearson相关系数
func (ca *CorrelationAnalyzer) pearsonCorrelation(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n == 0 {
		return 0
	}

	// 计算均值
	meanX, meanY := 0.0, 0.0
	for i := 0; i < n; i++ {
		meanX += x[i]
		meanY += y[i]
	}
	meanX /= float64(n)
	meanY /= float64(n)

	// 计算协方差和标准差
	cov, varX, varY := 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		cov += dx * dy
		varX += dx * dx
		varY += dy * dy
	}

	if varX == 0 || varY == 0 {
		return 0
	}

	return cov / math.Sqrt(varX*varY)
}

// BasisAnalyzer 基差分析器
type BasisAnalyzer struct{}

// NewBasisAnalyzer 创建基差分析器
func NewBasisAnalyzer() *BasisAnalyzer {
	return &BasisAnalyzer{}
}

// CalcBasis 计算基差
func (ba *BasisAnalyzer) CalcBasis(futuresPrice, spotPrice float64) float64 {
	if spotPrice == 0 {
		return 0
	}
	return (futuresPrice - spotPrice) / spotPrice
}
