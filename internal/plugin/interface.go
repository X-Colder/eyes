package plugin

import (
	"github.com/eyes/internal/model"
)

// InstrumentPlugin 品种插件接口
// 所有品种(股票、期货、期权)都实现此接口
type InstrumentPlugin interface {
	// Meta 返回品种元信息
	Meta() *model.InstrumentMeta

	// ValidateTick 验证Tick数据有效性
	ValidateTick(tick *model.TickData) error

	// ValidateBar 验证Bar数据有效性
	ValidateBar(bar *model.TickBar) error

	// CalcPositionValue 计算持仓价值
	CalcPositionValue(volume int64, price float64) float64

	// CalcRequiredMargin 计算所需保证金 (期货专用)
	CalcRequiredMargin(volume int64, price float64) float64

	// CalcPnL 计算盈亏
	CalcPnL(volume int64, entryPrice, exitPrice float64, direction string) float64

	// AdjustPrice 调整价格 (考虑滑点、买卖方向)
	AdjustPrice(price float64, isBuy bool, slippage float64) float64

	// CalcCommission 计算手续费
	CalcCommission(volume int64, price float64, isBuy bool) float64
}

// FeatureExtractor 特征提取器接口
type FeatureExtractor interface {
	// Extract 提取特征
	Extract(bars []model.TickBar) ([]model.Feature, error)

	// FeatureDim 返回特征维度
	FeatureDim() int

	// FeatureNames 返回特征名称
	FeatureNames() []string
}

// SignalGenerator 信号生成器接口
type SignalGenerator interface {
	// Generate 生成交易信号
	Generate(features []model.Feature, prediction *model.PredictionResult) (*model.TradeSignal, error)

	// ValidateSignal 验证信号有效性
	ValidateSignal(signal *model.TradeSignal) bool
}

// RiskManager 风险管理器接口
type RiskManager interface {
	// CheckPosition 检查持仓风险
	CheckPosition(position *model.Position) error

	// CalcMaxVolume 计算最大可开仓量
	CalcMaxVolume(cash float64, price float64) int64

	// ShouldStopLoss 是否应该止损
	ShouldStopLoss(position *model.Position, currentPrice float64) bool

	// ShouldTakeProfit 是否应该止盈
	ShouldTakeProfit(position *model.Position, currentPrice float64) bool
}

// DataAdapter 数据适配器接口
type DataAdapter interface {
	// LoadTicks 加载Tick数据
	LoadTicks(date, symbol string) ([]model.TickData, error)

	// LoadBars 加载Bar数据
	LoadBars(date, symbol string, interval int) ([]model.TickBar, error)

	// NormalizeTick 标准化Tick数据
	NormalizeTick(tick *model.TickData) error

	// AggregateBars 聚合Tick为Bar
	AggregateBars(ticks []model.TickData, interval int) []model.TickBar
}

// PluginRegistry 插件注册表
type PluginRegistry struct {
	instruments map[string]InstrumentPlugin
	extractors  map[string]FeatureExtractor
	generators  map[string]SignalGenerator
	risks       map[string]RiskManager
	adapters    map[string]DataAdapter
}

// NewPluginRegistry 创建插件注册表
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		instruments: make(map[string]InstrumentPlugin),
		extractors:  make(map[string]FeatureExtractor),
		generators:  make(map[string]SignalGenerator),
		risks:       make(map[string]RiskManager),
		adapters:    make(map[string]DataAdapter),
	}
}

// RegisterInstrument 注册品种插件
func (pr *PluginRegistry) RegisterInstrument(symbol string, plugin InstrumentPlugin) {
	pr.instruments[symbol] = plugin
}

// GetInstrument 获取品种插件
func (pr *PluginRegistry) GetInstrument(symbol string) (InstrumentPlugin, bool) {
	plugin, ok := pr.instruments[symbol]
	return plugin, ok
}

// RegisterExtractor 注册特征提取器
func (pr *PluginRegistry) RegisterExtractor(symbol string, extractor FeatureExtractor) {
	pr.extractors[symbol] = extractor
}

// GetExtractor 获取特征提取器
func (pr *PluginRegistry) GetExtractor(symbol string) (FeatureExtractor, bool) {
	ext, ok := pr.extractors[symbol]
	return ext, ok
}

// RegisterGenerator 注册信号生成器
func (pr *PluginRegistry) RegisterGenerator(symbol string, generator SignalGenerator) {
	pr.generators[symbol] = generator
}

// GetGenerator 获取信号生成器
func (pr *PluginRegistry) GetGenerator(symbol string) (SignalGenerator, bool) {
	gen, ok := pr.generators[symbol]
	return gen, ok
}

// RegisterRisk 注册风险管理器
func (pr *PluginRegistry) RegisterRisk(symbol string, risk RiskManager) {
	pr.risks[symbol] = risk
}

// GetRisk 获取风险管理器
func (pr *PluginRegistry) GetRisk(symbol string) (RiskManager, bool) {
	risk, ok := pr.risks[symbol]
	return risk, ok
}

// RegisterAdapter 注册数据适配器
func (pr *PluginRegistry) RegisterAdapter(symbol string, adapter DataAdapter) {
	pr.adapters[symbol] = adapter
}

// GetAdapter 获取数据适配器
func (pr *PluginRegistry) GetAdapter(symbol string) (DataAdapter, bool) {
	adapter, ok := pr.adapters[symbol]
	return adapter, ok
}
