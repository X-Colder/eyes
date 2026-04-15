package config

import (
	"encoding/json"
	"os"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig   `json:"server"`
	Data     DataConfig     `json:"data"`
	Feature  FeatureConfig  `json:"feature"`
	ML       MLConfig       `json:"ml"`
	Backtest BacktestConfig `json:"backtest"`
	Pipeline PipelineConfig `json:"pipeline"`
}

type ServerConfig struct {
	Port string `json:"port"`
}

type DataConfig struct {
	TickDir   string `json:"tick_dir"`   // tick 数据目录
	OutputDir string `json:"output_dir"` // 输出目录
}

type FeatureConfig struct {
	BarInterval int     `json:"bar_interval"` // 聚合秒数 (如 30s)
	WindowSize  int     `json:"window_size"`  // 滑动窗口大小
	FutureSteps int     `json:"future_steps"` // 预测未来N根bar
	PriceThresh float64 `json:"price_thresh"` // 涨跌标签阈值(%)
}

type MLConfig struct {
	ModelDir   string `json:"model_dir"`
	ScriptDir  string `json:"script_dir"`
	PythonPath string `json:"python_path"`
	ServiceURL string `json:"service_url"` // Python 推理服务地址
}

type BacktestConfig struct {
	InitialCash float64 `json:"initial_cash"`
	Commission  float64 `json:"commission"`   // 手续费率
	Slippage    float64 `json:"slippage"`     // 滑点
	MaxPosition int64   `json:"max_position"` // 最大持仓量
}

type PipelineConfig struct {
	Symbol       string  `json:"symbol"`        // 标的代码
	TrainRatio   float64 `json:"train_ratio"`   // 训练天数占比 (如 0.7)
	RetrainAfter int     `json:"retrain_after"` // 每推理 N 天后再训练 (0=不再训练)
	FeatureDim   int     `json:"feature_dim"`   // 特征维度 (24)
}

// Load 从 JSON 文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
