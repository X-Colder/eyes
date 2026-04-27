package engine

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/eyes/internal/model"
)

// OptimizerConfig 优化器配置
type OptimizerConfig struct {
	MaxTotalRisk       float64 `json:"max_total_risk"`      // 最大总风险敞口
	MaxSingleRisk      float64 `json:"max_single_risk"`     // 单品种最大风险
	MinSharpeRatio     float64 `json:"min_sharpe_ratio"`    // 最小夏普比率
	RiskFreeRate       float64 `json:"risk_free_rate"`      // 无风险利率
	MaxCorrelation     float64 `json:"max_correlation"`     // 最大相关性限制
	MinDiversification float64 `json:"min_diversification"` // 最小分散化度
}

// DefaultOptimizerConfig 默认优化器配置
func DefaultOptimizerConfig() *OptimizerConfig {
	return &OptimizerConfig{
		MaxTotalRisk:       0.3,  // 总风险不超过30%
		MaxSingleRisk:      0.15, // 单品种风险不超过15%
		MinSharpeRatio:     1.0,  // 最小夏普比率1.0
		RiskFreeRate:       0.03, // 无风险利率3%
		MaxCorrelation:     0.85, // 相关性不超过85%
		MinDiversification: 0.3,  // 最小分散化度30%
	}
}

// OptimizedPortfolio 优化后的组合
type OptimizedPortfolio struct {
	Timestamp      string                        `json:"timestamp"`
	Strategy       string                        `json:"strategy"`       // 策略类型: arbitrage/hedge/resonance/independent
	Signals        []*OptimizedSignal            `json:"signals"`        // 优化后的信号列表
	RiskMetrics    *PortfolioRiskMetrics         `json:"risk_metrics"`   // 风险指标
	ReturnMetrics  *PortfolioReturnMetrics       `json:"return_metrics"` // 收益指标
	Score          float64                       `json:"score"`          // 综合得分
	Correlations   map[string]map[string]float64 `json:"correlations"`   // 相关性矩阵
	Recommendation string                        `json:"recommendation"` // 推荐建议
}

// OptimizedSignal 优化后的信号
type OptimizedSignal struct {
	Symbol         string  `json:"symbol"`
	Action         string  `json:"action"`
	Volume         int64   `json:"volume"`
	OriginalVolume int64   `json:"original_volume"` // 原始建议量
	AdjustedRatio  float64 `json:"adjusted_ratio"`  // 调整比例
	ExpectedReturn float64 `json:"expected_return"`
	Risk           float64 `json:"risk"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	Confidence     float64 `json:"confidence"`
	Weight         float64 `json:"weight"` // 组合权重
}

// PortfolioRiskMetrics 组合风险指标
type PortfolioRiskMetrics struct {
	TotalRisk         float64 `json:"total_risk"`         // 总风险
	MaxDrawdown       float64 `json:"max_drawdown"`       // 最大回撤
	VaR95             float64 `json:"var_95"`             // 95% VaR
	Diversification   float64 `json:"diversification"`    // 分散化度
	CorrelationRisk   float64 `json:"correlation_risk"`   // 相关性风险
	ConcentrationRisk float64 `json:"concentration_risk"` // 集中度风险
}

// PortfolioReturnMetrics 组合收益指标
type PortfolioReturnMetrics struct {
	ExpectedReturn     float64 `json:"expected_return"`      // 预期收益
	RiskAdjustedReturn float64 `json:"risk_adjusted_return"` // 风险调整收益
	SharpeRatio        float64 `json:"sharpe_ratio"`         // 夏普比率
	SortinoRatio       float64 `json:"sortino_ratio"`        // 索提诺比率
	WinRate            float64 `json:"win_rate"`             // 胜率
}

// PortfolioOptimizer 组合优化器
type PortfolioOptimizer struct {
	config *OptimizerConfig
}

// NewPortfolioOptimizer 创建组合优化器
func NewPortfolioOptimizer(config *OptimizerConfig) *PortfolioOptimizer {
	if config == nil {
		config = DefaultOptimizerConfig()
	}
	return &PortfolioOptimizer{config: config}
}

// Optimize 优化组合
func (po *PortfolioOptimizer) Optimize(
	signals map[string][]model.TradeSignal,
	predictions map[string]*model.PredictionResult,
	positions map[string]*model.Position,
	correlations map[string]map[string]float64,
	basisMap map[string]float64,
	instruments map[string]*model.InstrumentMeta,
) *OptimizedPortfolio {

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// 1. 生成候选信号
	candidates := po.generateCandidates(signals, predictions, instruments)
	if len(candidates) == 0 {
		return &OptimizedPortfolio{
			Timestamp:      timestamp,
			Strategy:       "hold",
			Recommendation: "无有效信号,保持观望",
		}
	}

	// 2. 计算每个信号的风险收益特征
	po.calcSignalMetrics(candidates, positions, instruments)

	// 3. 识别策略类型
	strategy := po.identifyStrategy(candidates, correlations, basisMap)

	// 4. 根据策略优化仓位
	optimizedSignals := po.optimizeByStrategy(
		strategy, candidates, correlations, positions, instruments,
	)

	// 5. 计算组合风险指标
	riskMetrics := po.calcPortfolioRiskMetrics(
		optimizedSignals, correlations, positions, instruments,
	)

	// 6. 计算组合收益指标
	returnMetrics := po.calcPortfolioReturnMetrics(optimizedSignals)

	// 7. 计算综合得分
	score := po.calcCompositeScore(riskMetrics, returnMetrics)

	// 8. 生成推荐建议
	recommendation := po.generateRecommendation(strategy, riskMetrics, returnMetrics)

	return &OptimizedPortfolio{
		Timestamp:      timestamp,
		Strategy:       strategy,
		Signals:        optimizedSignals,
		RiskMetrics:    riskMetrics,
		ReturnMetrics:  returnMetrics,
		Score:          score,
		Correlations:   correlations,
		Recommendation: recommendation,
	}
}

// generateCandidates 生成候选信号
func (po *PortfolioOptimizer) generateCandidates(
	signals map[string][]model.TradeSignal,
	predictions map[string]*model.PredictionResult,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	var candidates []*OptimizedSignal

	for symbol, sigs := range signals {
		if len(sigs) == 0 {
			continue
		}

		latest := sigs[len(sigs)-1]
		if _, ok := predictions[symbol]; !ok {
			continue
		}

		if _, ok := instruments[symbol]; !ok {
			continue
		}

		// 只处理买入或卖出信号
		if latest.Action == "hold" {
			continue
		}

		candidate := &OptimizedSignal{
			Symbol:         symbol,
			Action:         latest.Action,
			Volume:         latest.Volume,
			OriginalVolume: latest.Volume,
			Confidence:     latest.Confidence,
			ExpectedReturn: latest.ProfitRate / 100, // 转为小数
		}

		candidates = append(candidates, candidate)
	}

	return candidates
}

// calcSignalMetrics 计算信号指标
func (po *PortfolioOptimizer) calcSignalMetrics(
	candidates []*OptimizedSignal,
	positions map[string]*model.Position,
	instruments map[string]*model.InstrumentMeta,
) {

	for _, sig := range candidates {
		meta := instruments[sig.Symbol]

		// 计算风险 (基于波动率估计,简化为置信度的倒数)
		sig.Risk = (1 - sig.Confidence) * 0.1 // 简化风险估计

		// 计算夏普比率
		if sig.Risk > 0 {
			sig.SharpeRatio = (sig.ExpectedReturn - po.config.RiskFreeRate) / sig.Risk
		}

		// 根据品种类型调整风险
		if meta.Type == model.TypeFuture {
			sig.Risk *= (1 / meta.MarginRatio) // 期货杠杆放大风险
		}
	}
}

// identifyStrategy 识别策略类型
func (po *PortfolioOptimizer) identifyStrategy(
	candidates []*OptimizedSignal,
	correlations map[string]map[string]float64,
	basisMap map[string]float64,
) string {

	// 1. 检查是否有套利机会 (基差异常)
	for _, basis := range basisMap {
		if math.Abs(basis) > 0.005 { // 基差 > 0.5%
			return "arbitrage"
		}
	}

	// 2. 检查是否有高相关性品种 (可以做对冲)
	hasHighCorr := false
	for sym1, corrMap := range correlations {
		for sym2, corr := range corrMap {
			if corr > po.config.MaxCorrelation && sym1 != sym2 {
				hasHighCorr = true
				break
			}
		}
	}

	// 检查是否有相反方向的信号
	if hasHighCorr && len(candidates) >= 2 {
		hasBuy := false
		hasSell := false
		for _, sig := range candidates {
			if sig.Action == "buy" {
				hasBuy = true
			}
			if sig.Action == "sell" {
				hasSell = true
			}
		}
		if hasBuy && hasSell {
			return "hedge"
		}
	}

	// 3. 检查是否有共振机会 (高相关性 + 同方向)
	if hasHighCorr && len(candidates) >= 2 {
		allSameDir := true
		firstAction := candidates[0].Action
		for _, sig := range candidates {
			if sig.Action != firstAction {
				allSameDir = false
				break
			}
		}
		if allSameDir {
			return "resonance"
		}
	}

	// 4. 默认独立交易
	return "independent"
}

// optimizeByStrategy 根据策略优化仓位
func (po *PortfolioOptimizer) optimizeByStrategy(
	strategy string,
	candidates []*OptimizedSignal,
	correlations map[string]map[string]float64,
	positions map[string]*model.Position,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	switch strategy {
	case "arbitrage":
		return po.optimizeArbitrage(candidates, correlations, instruments)
	case "hedge":
		return po.optimizeHedge(candidates, correlations, instruments)
	case "resonance":
		return po.optimizeResonance(candidates, correlations, instruments)
	default:
		return po.optimizeIndependent(candidates, instruments)
	}
}

// optimizeArbitrage 套利策略优化
func (po *PortfolioOptimizer) optimizeArbitrage(
	candidates []*OptimizedSignal,
	correlations map[string]map[string]float64,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	// 套利: 风险低,仓位可以更大
	for _, sig := range candidates {
		sig.AdjustedRatio = 1.0
		sig.Weight = 1.0 / float64(len(candidates))
		sig.Risk *= 0.3 // 套利风险降低70%
		sig.SharpeRatio = (sig.ExpectedReturn - po.config.RiskFreeRate) / sig.Risk
	}

	return candidates
}

// optimizeHedge 对冲策略优化
func (po *PortfolioOptimizer) optimizeHedge(
	candidates []*OptimizedSignal,
	correlations map[string]map[string]float64,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	// 对冲: 根据相关性调整对冲比例
	for i, sig1 := range candidates {
		for j, sig2 := range candidates {
			if i >= j {
				continue
			}

			corr := correlations[sig1.Symbol][sig2.Symbol]

			// 相反方向的信号,根据相关性调整比例
			if sig1.Action != sig2.Action && corr > 0.5 {
				// 高相关性,对冲效果好
				sig1.AdjustedRatio = corr
				sig2.AdjustedRatio = corr
				sig1.Weight = 0.5
				sig2.Weight = 0.5

				// 风险抵消
				sig1.Risk *= (1 - corr)
				sig2.Risk *= (1 - corr)
			}
		}
	}

	return candidates
}

// optimizeResonance 共振策略优化
func (po *PortfolioOptimizer) optimizeResonance(
	candidates []*OptimizedSignal,
	correlations map[string]map[string]float64,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	// 共振: 风险叠加,需要降低仓位
	for _, sig := range candidates {
		// 计算平均相关性
		avgCorr := 0.0
		count := 0
		for sym2, corr := range correlations[sig.Symbol] {
			if sym2 != sig.Symbol {
				avgCorr += corr
				count++
			}
		}
		if count > 0 {
			avgCorr /= float64(count)
		}

		// 相关性越高,仓位降低越多
		sig.AdjustedRatio = 1.0 - avgCorr*0.5
		sig.Weight = 1.0 / float64(len(candidates))
		sig.Volume = int64(float64(sig.OriginalVolume) * sig.AdjustedRatio)

		// 风险叠加
		sig.Risk *= (1 + avgCorr)
	}

	return candidates
}

// optimizeIndependent 独立策略优化
func (po *PortfolioOptimizer) optimizeIndependent(
	candidates []*OptimizedSignal,
	instruments map[string]*model.InstrumentMeta,
) []*OptimizedSignal {

	// 独立交易: 根据夏普比率优化权重
	totalSharpe := 0.0
	for _, sig := range candidates {
		if sig.SharpeRatio > 0 {
			totalSharpe += sig.SharpeRatio
		}
	}

	for _, sig := range candidates {
		sig.AdjustedRatio = 1.0

		// 按夏普比率分配权重
		if totalSharpe > 0 && sig.SharpeRatio > 0 {
			sig.Weight = sig.SharpeRatio / totalSharpe
		} else {
			sig.Weight = 1.0 / float64(len(candidates))
		}

		// 限制单品种风险
		if sig.Risk > po.config.MaxSingleRisk {
			sig.Volume = int64(float64(sig.Volume) * po.config.MaxSingleRisk / sig.Risk)
			sig.AdjustedRatio = po.config.MaxSingleRisk / sig.Risk
		}
	}

	return candidates
}

// calcPortfolioRiskMetrics 计算组合风险指标
func (po *PortfolioOptimizer) calcPortfolioRiskMetrics(
	signals []*OptimizedSignal,
	correlations map[string]map[string]float64,
	positions map[string]*model.Position,
	instruments map[string]*model.InstrumentMeta,
) *PortfolioRiskMetrics {

	metrics := &PortfolioRiskMetrics{}

	// 计算总风险 (考虑相关性)
	totalRisk := 0.0
	for _, sig := range signals {
		totalRisk += sig.Risk * sig.Risk * sig.Weight * sig.Weight
	}

	// 考虑协方差项
	for i, sig1 := range signals {
		for j, sig2 := range signals {
			if i >= j {
				continue
			}
			corr := correlations[sig1.Symbol][sig2.Symbol]
			totalRisk += 2 * sig1.Risk * sig2.Risk * corr * sig1.Weight * sig2.Weight
		}
	}

	metrics.TotalRisk = math.Sqrt(totalRisk)

	// 计算分散化度
	diversification := 0.0
	if len(signals) > 1 {
		avgCorr := 0.0
		count := 0
		for _, sig1 := range signals {
			for _, sig2 := range signals {
				if sig1.Symbol != sig2.Symbol {
					avgCorr += math.Abs(correlations[sig1.Symbol][sig2.Symbol])
					count++
				}
			}
		}
		if count > 0 {
			avgCorr /= float64(count)
			diversification = 1 - avgCorr
		}
	}
	metrics.Diversification = diversification

	// 简化计算其他指标
	metrics.MaxDrawdown = metrics.TotalRisk * 2
	metrics.VaR95 = metrics.TotalRisk * 1.65
	metrics.CorrelationRisk = metrics.TotalRisk * 0.3
	metrics.ConcentrationRisk = metrics.TotalRisk * 0.2

	return metrics
}

// calcPortfolioReturnMetrics 计算组合收益指标
func (po *PortfolioOptimizer) calcPortfolioReturnMetrics(
	signals []*OptimizedSignal,
) *PortfolioReturnMetrics {

	metrics := &PortfolioReturnMetrics{}

	// 加权预期收益
	expectedReturn := 0.0
	for _, sig := range signals {
		expectedReturn += sig.ExpectedReturn * sig.Weight
	}
	metrics.ExpectedReturn = expectedReturn

	// 加权夏普比率
	avgSharpe := 0.0
	for _, sig := range signals {
		avgSharpe += sig.SharpeRatio * sig.Weight
	}
	metrics.SharpeRatio = avgSharpe

	// 风险调整收益
	totalRisk := 0.0
	for _, sig := range signals {
		totalRisk += sig.Risk * sig.Weight
	}
	if totalRisk > 0 {
		metrics.RiskAdjustedReturn = expectedReturn / totalRisk
	}

	// 简化计算其他指标
	metrics.SortinoRatio = metrics.SharpeRatio * 1.2
	metrics.WinRate = 0.6 // 简化估计

	return metrics
}

// calcCompositeScore 计算综合得分
func (po *PortfolioOptimizer) calcCompositeScore(
	risk *PortfolioRiskMetrics,
	ret *PortfolioReturnMetrics,
) float64 {

	// 综合得分 = 夏普比率 * 分散化度 / 总风险
	score := ret.SharpeRatio * risk.Diversification
	if risk.TotalRisk > 0 {
		score /= risk.TotalRisk
	}

	// 归一化到 0-10
	score = math.Min(score*10, 10)
	score = math.Max(score, 0)

	return score
}

// generateRecommendation 生成推荐建议
func (po *PortfolioOptimizer) generateRecommendation(
	strategy string,
	risk *PortfolioRiskMetrics,
	ret *PortfolioReturnMetrics,
) string {

	reason := ""

	switch strategy {
	case "arbitrage":
		reason = fmt.Sprintf("基差异常,存在套利机会,预期收益%.2f%%,风险极低", ret.ExpectedReturn*100)
	case "hedge":
		reason = fmt.Sprintf("高相关性品种,对冲策略降低风险,风险调整收益%.2f", ret.RiskAdjustedReturn)
	case "resonance":
		reason = fmt.Sprintf("多品种趋势共振,预期收益%.2f%%,夏普比率%.2f", ret.ExpectedReturn*100, ret.SharpeRatio)
	default:
		reason = fmt.Sprintf("独立交易机会,预期收益%.2f%%,风险%.2f%%", ret.ExpectedReturn*100, risk.TotalRisk*100)
	}

	// 风险提示
	if risk.TotalRisk > po.config.MaxTotalRisk {
		reason += " [警告: 风险超标,建议降低仓位]"
	}

	if ret.SharpeRatio < po.config.MinSharpeRatio {
		reason += " [注意: 夏普比率偏低]"
	}

	return reason
}

// SortSignalsBySharpe 按夏普比率排序信号
func (po *PortfolioOptimizer) SortSignalsBySharpe(signals []*OptimizedSignal) {
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].SharpeRatio > signals[j].SharpeRatio
	})
}
