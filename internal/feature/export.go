package feature

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/eyes/internal/model"
)

// ExportData 导出结构，供 Python 侧消费
type ExportData struct {
	Symbol     string           `json:"symbol"`
	Date       string           `json:"date"`
	BarCount   int              `json:"bar_count"`
	FeatureDim int              `json:"feature_dim"`
	WindowSize int              `json:"window_size"`
	Bars       []model.TickBar  `json:"bars"`
	Features   []model.Feature  `json:"features"`
	Stats      model.DailyStats `json:"stats"`
}

// ExportToJSON 将特征数据导出为 JSON 文件
func ExportToJSON(data *ExportData, filePath string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create file %s: %w", filePath, err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

// ExportFeaturesCSV 导出特征为 CSV（供 Python 训练用）
// CSV 包含 date, symbol, time 列 + 特征列 + label, price_chg, phase
func ExportFeaturesCSV(features []model.Feature, filePath string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create file %s: %w", filePath, err)
	}
	defer f.Close()

	if len(features) == 0 {
		return fmt.Errorf("no features to export")
	}

	// 写表头
	dim := len(features[0].Values)
	header := "date,symbol,time"
	for i := 0; i < dim; i++ {
		header += fmt.Sprintf(",f%d", i)
	}
	header += ",label,price_chg,phase\n"
	if _, err := f.WriteString(header); err != nil {
		return err
	}

	// 写数据
	for _, feat := range features {
		line := fmt.Sprintf("%s,%s,%s", feat.Date, feat.Symbol, feat.Time)
		for _, v := range feat.Values {
			line += fmt.Sprintf(",%.6f", v)
		}
		line += fmt.Sprintf(",%d,%.6f,%d\n", feat.Label, feat.PriceChg, feat.Phase)
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}

// ExportMultiDayFeaturesCSV 将多日特征合并导出为单个 CSV
func ExportMultiDayFeaturesCSV(multiDay *model.MultiDayData, filePath string) error {
	if multiDay == nil || len(multiDay.Features) == 0 {
		return fmt.Errorf("no features to export")
	}
	return ExportFeaturesCSV(multiDay.Features, filePath)
}
