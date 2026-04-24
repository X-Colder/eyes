package plugin

import (
	"errors"

	"github.com/eyes/internal/model"
)

// FuturesPlugin 期货品种插件
type FuturesPlugin struct {
	meta       *model.InstrumentMeta
	commission float64 // 手续费率
	slippage   float64 // 滑点
}

// NewFuturesPlugin 创建期货插件
func NewFuturesPlugin(meta *model.InstrumentMeta, commission, slippage float64) *FuturesPlugin {
	return &FuturesPlugin{
		meta:       meta,
		commission: commission,
		slippage:   slippage,
	}
}

// Meta 返回品种元信息
func (fp *FuturesPlugin) Meta() *model.InstrumentMeta {
	return fp.meta
}

// ValidateTick 验证Tick数据
func (fp *FuturesPlugin) ValidateTick(tick *model.TickData) error {
	if tick.Price <= 0 {
		return errors.New("invalid price")
	}
	if tick.Volume <= 0 {
		return errors.New("invalid volume")
	}
	// 期货特有: 持仓量验证
	if tick.OpenInterest < 0 {
		return errors.New("invalid open interest")
	}
	return nil
}

// ValidateBar 验证Bar数据
func (fp *FuturesPlugin) ValidateBar(bar *model.TickBar) error {
	if bar.Open <= 0 || bar.Close <= 0 {
		return errors.New("invalid price")
	}
	if bar.Volume < 0 {
		return errors.New("invalid volume")
	}
	// 期货特有: 持仓量验证
	if bar.OpenInterest < 0 {
		return errors.New("invalid open interest")
	}
	return nil
}

// CalcPositionValue 计算持仓价值 (合约价值 = 价格 × 合约乘数)
func (fp *FuturesPlugin) CalcPositionValue(volume int64, price float64) float64 {
	return float64(volume) * price * fp.meta.Multiplier
}

// CalcRequiredMargin 计算所需保证金
func (fp *FuturesPlugin) CalcRequiredMargin(volume int64, price float64) float64 {
	positionValue := fp.CalcPositionValue(volume, price)
	return positionValue * fp.meta.MarginRatio
}

// CalcPnL 计算盈亏 (考虑合约乘数)
func (fp *FuturesPlugin) CalcPnL(volume int64, entryPrice, exitPrice float64, direction string) float64 {
	pnl := (exitPrice - entryPrice) * float64(volume) * fp.meta.Multiplier
	if direction == "sell" {
		pnl = -pnl
	}
	return pnl
}

// AdjustPrice 调整价格 (滑点)
func (fp *FuturesPlugin) AdjustPrice(price float64, isBuy bool, slippage float64) float64 {
	if isBuy {
		return price * (1 + slippage)
	}
	return price * (1 - slippage)
}

// CalcCommission 计算手续费
func (fp *FuturesPlugin) CalcCommission(volume int64, price float64, isBuy bool) float64 {
	value := fp.CalcPositionValue(volume, price)
	return value * fp.commission
}

// CalcMaxLeverage 计算最大杠杆
func (fp *FuturesPlugin) CalcMaxLeverage() float64 {
	return 1.0 / fp.meta.MarginRatio
}
