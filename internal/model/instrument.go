package model

// InstrumentType 标的类型
type InstrumentType string

const (
	TypeStock  InstrumentType = "stock"
	TypeFuture InstrumentType = "future"
	TypeOption InstrumentType = "option"
)

// InstrumentMeta 标的元信息
type InstrumentMeta struct {
	Symbol           string         `json:"symbol"`             // 代码
	Name             string         `json:"name"`               // 名称
	Type             InstrumentType `json:"type"`               // 类型
	Exchange         string         `json:"exchange"`           // 交易所
	Multiplier       float64        `json:"multiplier"`         // 合约乘数 (期货)
	MarginRatio      float64        `json:"margin_ratio"`       // 保证金比例 (期货)
	MinTick          float64        `json:"min_tick"`           // 最小变动价位
	TradingHours     string         `json:"trading_hours"`      // 交易时段
	LotSize          int64          `json:"lot_size"`           // 每手股数/手数
	Enabled          bool           `json:"enabled"`            // 是否启用
	UnderlyingSymbol string         `json:"underlying_symbol"`  // 标的代码 (期权)
	
	// 特征配置
	FeatureDim       int            `json:"feature_dim"`        // 特征维度
	WindowSize       int            `json:"window_size"`        // 窗口大小
	BarInterval      int            `json:"bar_interval"`       // Bar间隔(秒)
	
	// 风控参数
	MaxPosition      int64          `json:"max_position"`       // 最大持仓
	MaxLeverage      float64        `json:"max_leverage"`       // 最大杠杆
	StopLossRatio    float64        `json:"stop_loss_ratio"`    // 止损比例
	TakeProfitRatio  float64        `json:"take_profit_ratio"`  // 止盈比例
}

// InstrumentRegistry 标的注册表
type InstrumentRegistry struct {
	instruments map[string]*InstrumentMeta
}

// NewInstrumentRegistry 创建标的注册表
func NewInstrumentRegistry() *InstrumentRegistry {
	return &InstrumentRegistry{
		instruments: make(map[string]*InstrumentMeta),
	}
}

// Register 注册标的
func (ir *InstrumentRegistry) Register(meta *InstrumentMeta) {
	ir.instruments[meta.Symbol] = meta
}

// Get 获取标的
func (ir *InstrumentRegistry) Get(symbol string) (*InstrumentMeta, bool) {
	meta, ok := ir.instruments[symbol]
	return meta, ok
}

// GetAll 获取所有启用的标的
func (ir *InstrumentRegistry) GetAll() []*InstrumentMeta {
	var list []*InstrumentMeta
	for _, meta := range ir.instruments {
		if meta.Enabled {
			list = append(list, meta)
		}
	}
	return list
}

// GetByType 按类型获取标的
func (ir *InstrumentRegistry) GetByType(instType InstrumentType) []*InstrumentMeta {
	var list []*InstrumentMeta
	for _, meta := range ir.instruments {
		if meta.Type == instType && meta.Enabled {
			list = append(list, meta)
		}
	}
	return list
}