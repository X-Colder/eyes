package loader

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/eyes/internal/model"
)

// ---------- 单文件加载 ----------

// LoadTickCSV 加载 tick 级 CSV 文件，返回按时间排序的 TickData 切片
func LoadTickCSV(filePath string) ([]model.TickData, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // 允许列数不一致（跳过损坏行）
	// 读取表头
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	log.Printf("[loader] CSV header: %v", header)

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read records: %w", err)
	}

	ticks := make([]model.TickData, 0, len(records))
	for i, rec := range records {
		if len(rec) < 11 {
			log.Printf("[loader] skip row %d: insufficient columns (%d)", i+2, len(rec))
			continue
		}

		tick, err := parseTickRow(rec)
		if err != nil {
			log.Printf("[loader] skip row %d: %v", i+2, err)
			continue
		}
		ticks = append(ticks, tick)
	}

	// 按时间 + TranID 排序
	sort.Slice(ticks, func(i, j int) bool {
		if ticks[i].Time == ticks[j].Time {
			return ticks[i].TranID < ticks[j].TranID
		}
		return ticks[i].Time < ticks[j].Time
	})

	log.Printf("[loader] loaded %d ticks from %s", len(ticks), filePath)
	return ticks, nil
}

// parseTickRow 解析 CSV 中的一行为 TickData
// 列顺序：TranID,Time,Price,Volume,SaleOrderVolume,BuyOrderVolume,Type,SaleOrderID,SaleOrderPrice,BuyOrderID,BuyOrderPrice
func parseTickRow(rec []string) (model.TickData, error) {
	var t model.TickData
	var err error

	t.TranID, err = strconv.ParseInt(strings.TrimSpace(rec[0]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse TranID: %w", err)
	}
	t.Time = strings.TrimSpace(rec[1])

	t.Price, err = strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)
	if err != nil {
		return t, fmt.Errorf("parse Price: %w", err)
	}
	t.Volume, err = strconv.ParseInt(strings.TrimSpace(rec[3]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse Volume: %w", err)
	}
	t.SaleOrderVolume, err = strconv.ParseInt(strings.TrimSpace(rec[4]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderVolume: %w", err)
	}
	t.BuyOrderVolume, err = strconv.ParseInt(strings.TrimSpace(rec[5]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderVolume: %w", err)
	}
	t.Type = strings.TrimSpace(rec[6])

	t.SaleOrderID, err = strconv.ParseInt(strings.TrimSpace(rec[7]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderID: %w", err)
	}
	t.SaleOrderPrice, err = strconv.ParseFloat(strings.TrimSpace(rec[8]), 64)
	if err != nil {
		return t, fmt.Errorf("parse SaleOrderPrice: %w", err)
	}
	t.BuyOrderID, err = strconv.ParseInt(strings.TrimSpace(rec[9]), 10, 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderID: %w", err)
	}
	t.BuyOrderPrice, err = strconv.ParseFloat(strings.TrimSpace(rec[10]), 64)
	if err != nil {
		return t, fmt.Errorf("parse BuyOrderPrice: %w", err)
	}

	return t, nil
}

// ---------- 多日批量加载 ----------

// csvFileInfo 存储从文件名解析出的元信息
type csvFileInfo struct {
	Path   string
	Symbol string
	Date   string // YYYY-MM-DD，无法解析时为空
}

// 文件名模式：
//
//	002484_2018-05-18.csv  -> symbol=002484, date=2018-05-18
//	002484_20180518.csv    -> symbol=002484, date=2018-05-18
//	002484.csv             -> symbol=002484, date=""（需要外部指定）
var (
	reNameWithDate = regexp.MustCompile(`^(\d{6})[-_](\d{4})-?(\d{2})-?(\d{2})\.csv$`)
	reNameOnly     = regexp.MustCompile(`^(\d{6})\.csv$`)
)

// parseFileName 从文件名提取 symbol 和 date
func parseFileName(name string) (symbol, date string) {
	if m := reNameWithDate.FindStringSubmatch(name); m != nil {
		return m[1], fmt.Sprintf("%s-%s-%s", m[2], m[3], m[4])
	}
	if m := reNameOnly.FindStringSubmatch(name); m != nil {
		return m[1], ""
	}
	return "", ""
}

// LoadMultiDayDir 扫描目录中所有 CSV 文件，按日期排序加载
// symbol: 要加载的标的代码（如 "002484"），为空则加载全部
// defaultDate: 当文件名中无法解析日期时使用的默认日期
func LoadMultiDayDir(dir string, symbol string, defaultDate string) ([]model.DayTicks, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var files []csvFileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			continue
		}
		sym, date := parseFileName(e.Name())
		if sym == "" {
			continue // 无法解析的文件跳过
		}
		if symbol != "" && sym != symbol {
			continue // 不匹配的标的跳过
		}
		if date == "" {
			date = defaultDate
		}
		files = append(files, csvFileInfo{
			Path:   filepath.Join(dir, e.Name()),
			Symbol: sym,
			Date:   date,
		})
	}

	// 按日期排序
	sort.Slice(files, func(i, j int) bool {
		return files[i].Date < files[j].Date
	})

	log.Printf("[loader] found %d CSV files in %s for symbol=%s", len(files), dir, symbol)

	var result []model.DayTicks
	for _, fi := range files {
		ticks, err := LoadTickCSV(fi.Path)
		if err != nil {
			log.Printf("[loader] WARN: skip %s: %v", fi.Path, err)
			continue
		}
		result = append(result, model.DayTicks{
			Date:   fi.Date,
			Symbol: fi.Symbol,
			Ticks:  ticks,
		})
		log.Printf("[loader] loaded %s: date=%s, ticks=%d", fi.Path, fi.Date, len(ticks))
	}

	return result, nil
}

// GetDailyStats 从 tick 数据计算日度统计
func GetDailyStats(ticks []model.TickData, symbol string) model.DailyStats {
	stats := model.DailyStats{Symbol: symbol, TotalTicks: len(ticks)}
	if len(ticks) == 0 {
		return stats
	}

	stats.OpenPrice = ticks[0].Price
	stats.ClosePrice = ticks[len(ticks)-1].Price
	stats.HighPrice = ticks[0].Price
	stats.LowPrice = ticks[0].Price

	for _, t := range ticks {
		stats.TotalVolume += t.Volume
		if t.Price > stats.HighPrice {
			stats.HighPrice = t.Price
			stats.HighTime = t.Time
		}
		if t.Price < stats.LowPrice {
			stats.LowPrice = t.Price
			stats.LowTime = t.Time
		}
	}
	if stats.LowPrice > 0 {
		stats.Amplitude = (stats.HighPrice - stats.LowPrice) / stats.LowPrice * 100
	}
	return stats
}
