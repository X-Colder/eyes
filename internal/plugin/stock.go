package plugin

import (
	"errors"

	"github.com/eyes/internal/model"
)

// StockPlugin 股票品种插件
type StockPlugin struct {
	meta       *model.InstrumentMeta
	commission float64 // 手续费率
	slippage   float64 // 滑点
}

// NewStockPlugin 创建股票插件
func NewStockPlugin(meta *model.InstrumentMeta, commission, slippage float64) *StockPlugin {
	return &StockPlugin{
		meta:       meta,
		commission: commission,
		slippage:   slippage,
	}
}

// Meta 返回品种元信息
func (sp *StockPlugin) Meta() *model.InstrumentMeta {
	return sp.meta
}

// ValidateTick 验证Tick数据
func (sp *StockPlugin) ValidateTick(tick *model.TickData) error {
	if tick.Price <= 0 {
		return errors.New("invalid price")
	}
	if tick.Volume <= 0 {
		return errors.New("invalid volume")
	}
	return nil
}

// ValidateBar 验证Bar数据
func (sp *StockPlugin) ValidateBar(bar *model.TickBar) error {
	if bar.Open <= 0 || bar.Close <= 0 {
		return errors.New("invalid price")
	}
	if bar.Volume < 0 {
		return errors.New("invalid volume")
	}
	return nil
}

// CalcPositionValue 计算持仓价值
func (sp *StockPlugin) CalcPositionValue(volume int64, price float64) float64 {
	return float64(volume) * price
}

// CalcRequiredMargin 股票无保证金概念,返回持仓价值
func (sp *StockPlugin) CalcRequiredMargin(volume int64, price float64) float64 {
	return sp.CalcPositionValue(volume, price)
}

// CalcPnL 计算盈亏
func (sp *StockPlugin) CalcPnL(volume int64, entryPrice, exitPrice float64, direction string) float64 {
	pnl := (exitPrice - entryPrice) * float64(volume)
	if direction == "sell" {
		pnl = -pnl
	}
	return pnl
}

// AdjustPrice 调整价格 (滑点)
func (sp *StockPlugin) AdjustPrice(price float64, isBuy bool, slippage float64) float64 {
	if isBuy {
		return price * (1 + slippage) // 买入价格上浮
	}
	return price * (1 - slippage) // 卖出价格下浮
}

// CalcCommission 计算手续费
func (sp *StockPlugin) CalcCommission(volume int64, price float64, isBuy bool) float64 {
	value := sp.CalcPositionValue(volume, price)
	return value * sp.commission
}
