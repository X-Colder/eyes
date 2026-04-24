#!/usr/bin/env python3
"""
将JSON测试结果转换为Markdown格式报告
"""

import json
import os
import sys
from datetime import datetime

def load_result(file_path):
    """加载测试结果JSON文件"""
    with open(file_path, 'r', encoding='utf-8') as f:
        return json.load(f)

def generate_markdown_report(result, output_path=None):
    """生成Markdown格式测试报告"""
    config = result['Config']
    start_time = datetime.fromisoformat(result['StartTime'].replace('Z', '+00:00'))
    end_time = datetime.fromisoformat(result['EndTime'].replace('Z', '+00:00'))
    duration = (end_time - start_time).total_seconds()

    # 计算统计数据
    total_trades = result['TradeCount']
    winning_trades = sum(1 for t in result['Trades'] if t['PnL'] > 0)
    losing_trades = sum(1 for t in result['Trades'] if t['PnL'] < 0)
    avg_win = sum(t['PnL'] for t in result['Trades'] if t['PnL'] > 0) / max(winning_trades, 1)
    avg_loss = sum(abs(t['PnL']) for t in result['Trades'] if t['PnL'] < 0) / max(losing_trades, 1)
    profit_factor = avg_win / max(avg_loss, 0.01) if avg_loss > 0 else float('inf')

    # 生成报告内容
    md = f"""# {config['Name']} 测试报告

## 📊 测试基本信息
| 项⽬ | 内容 |
|------|------|
| 测试名称 | {config['Name']} |
| 测试描述 | {config['Description']} |
| 测试时间 | {start_time.strftime('%Y-%m-%d %H:%M:%S')} |
| 测试时长 | {duration:.2f} 秒 |
| 测试模式 | {config['Mode']} |
| 错误率 | {config['ErrorRate']:.1%} |
| 初始资金 | ¥{config['InitialCash']:,.2f} |
| 手续费率 | {config['Commission']:.2%} |
| 滑点 | {config['Slippage']:.2%} |
| 最大持仓 | {config['MaxPosition']:,} 股 |
| Bar间隔 | {config['BarInterval']} 秒 |
| 窗口大小 | {config['WindowSize']} 根Bar |
| 预测步数 | {config['FutureSteps']} 根Bar |
| 涨跌阈值 | {config['PriceThresh']:.2%} |

## 📈 测试结果汇总
| 指标 | 数值 |
|------|------|
| 总收益率 | **{result['TotalReturn']:.2f}%** |
| 总盈亏 | **¥{result['TotalPnL']:,.2f}** |
| 交易次数 | {result['TradeCount']} 次 |
| 盈利交易 | {winning_trades} 次 |
| 亏损交易 | {losing_trades} 次 |
| 胜率 | {result['WinRate']:.1f}% |
| 最大回撤 | {result['MaxDrawdown']:.2f}% |
| 平均盈利 | ¥{avg_win:,.2f} |
| 平均亏损 | ¥{avg_loss:,.2f} |
| 盈亏比 | {profit_factor:.2f} |
| 处理Bar数 | {result['BarCount']} 根 |
| 生成信号数 | {result['SignalCount']} 个 |

## 📝 交易明细
| 序号 | 开仓时间 | 平仓时间 | 开仓价(¥) | 平仓价(¥) | 交易量(股) | 盈亏(¥) | 收益率(%) | 持仓Bar数 |
|------|----------|----------|-----------|-----------|------------|---------|-----------|----------|
"""

    # 添加交易明细
    for idx, trade in enumerate(result['Trades'], 1):
        pnl_color = "🟢" if trade['PnL'] > 0 else "🔴"
        md += f"| {idx} | {trade['EntryTime']} | {trade['ExitTime']} | {trade['EntryPrice']:.2f} | {trade['ExitPrice']:.2f} | {trade['Volume']:,} | {pnl_color} {trade['PnL']:,.2f} | {trade['PnLPct']:.2f} | {trade['HoldBars']} |\n"

    # 添加分析结论
    md += """
## 🎯 测试结论分析
"""

    # 自动生成分析结论
    if result['TotalReturn'] > 10:
        md += "- ✅ **优秀**: 收益率超过10%，表现优异\n"
    elif result['TotalReturn'] > 0:
        md += "- ✅ **良好**: 实现正收益，表现符合预期\n"
    elif result['TotalReturn'] > -10:
        md += "- ⚠️ **一般**: 小幅亏损，在可接受范围内\n"
    else:
        md += "- ❌ **较差**: 亏损超过10%，需要优化策略\n"

    if result['WinRate'] > 60:
        md += "- ✅ **胜率优秀**: 超过60%，策略判断准确性高\n"
    elif result['WinRate'] > 50:
        md += "- ✅ **胜率良好**: 超过50%，判断正确次数居多\n"
    else:
        md += "- ⚠️ **胜率一般**: 低于50%，需要提升预测准确性\n"

    if result['MaxDrawdown'] < 10:
        md += "- ✅ **风险控制优秀**: 最大回撤低于10%，风险可控\n"
    elif result['MaxDrawdown'] < 20:
        md += "- ⚠️ **风险控制一般**: 最大回撤在10%-20%之间\n"
    else:
        md += "- ❌ **风险控制较差**: 最大回撤超过20%，需要优化止损策略\n"

    if profit_factor > 1.5:
        md += "- ✅ **盈亏比优秀**: 超过1.5，盈利能力显著高于亏损\n"
    elif profit_factor > 1.0:
        md += "- ✅ **盈亏比良好**: 大于1.0，长期来看可以盈利\n"
    else:
        md += "- ⚠️ **盈亏比不足**: 低于1.0，需要优化止盈止损策略\n"

    md += """
## 💡 优化建议
"""

    # 自动生成优化建议
    if result['WinRate'] < 50:
        md += "1. **提升预测准确率**: 当前胜率低于50%，建议优化模型特征或训练数据\n"
    if result['MaxDrawdown'] > 15:
        md += "2. **优化风险控制**: 最大回撤较高，建议调整止损参数或降低仓位比例\n"
    if profit_factor < 1.2:
        md += "3. **改善盈亏比**: 尝试扩大盈利幅度，限制亏损额度\n"
    if total_trades > 30:
        md += "4. **降低交易频率**: 交易次数过多，手续费成本可能较高，建议提高信号质量\n"
    if total_trades < 5:
        md += "4. **提高信号灵敏度**: 交易次数过少，可能错过交易机会\n"

    md += f"""
---
*报告生成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}*
"""

    # 保存报告
    if output_path:
        with open(output_path, 'w', encoding='utf-8') as f:
            f.write(md)
        print(f"报告已生成: {output_path}")

    return md

def main():
    if len(sys.argv) < 2:
        print("使用方法: python generate_report.py <结果JSON文件> [输出MD文件]")
        print("示例: python generate_report.py results/simulation_result.json reports/simulation_report.md")
        sys.exit(1)

    input_path = sys.argv[1]
    if not os.path.exists(input_path):
        print(f"错误: 文件不存在 {input_path}")
        sys.exit(1)

    output_path = sys.argv[2] if len(sys.argv) > 2 else input_path.replace('.json', '.md')
    
    # 确保输出目录存在
    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)

    result = load_result(input_path)
    generate_markdown_report(result, output_path)

if __name__ == "__main__":
    main()