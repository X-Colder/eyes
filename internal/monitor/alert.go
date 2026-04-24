package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// AlertType 报警类型
type AlertType string

const (
	AlertDrawdown   AlertType = "drawdown"    // 回撤报警
	AlertLoss       AlertType = "loss"        // 亏损报警
	AlertRisk       AlertType = "risk"        // 风险报警
	AlertMargin     AlertType = "margin"      // 保证金报警
	AlertModelError AlertType = "model_error" // 模型错误
	AlertDataFeed   AlertType = "data_feed"   // 数据源异常
	AlertSystem     AlertType = "system"      // 系统异常
	AlertProfit     AlertType = "profit"      // 盈利提醒
	AlertSignal     AlertType = "signal"      // 信号提醒
)

// AlertLevel 报警级别
type AlertLevel string

const (
	LevelInfo      AlertLevel = "info"
	LevelWarning   AlertLevel = "warning"
	LevelCritical  AlertLevel = "critical"
	LevelEmergency AlertLevel = "emergency"
)

// Alert 报警消息
type Alert struct {
	ID           string     `json:"id"`
	Type         AlertType  `json:"type"`
	Level        AlertLevel `json:"level"`
	Title        string     `json:"title"`
	Message      string     `json:"message"`
	Symbol       string     `json:"symbol"`
	Value        float64    `json:"value"`
	Threshold    float64    `json:"threshold"`
	Timestamp    string     `json:"timestamp"`
	Acknowledged bool       `json:"acknowledged"`
	Channels     []string   `json:"channels"`
}

// AlertConfig 报警配置
type AlertConfig struct {
	// 邮件配置
	EmailEnabled bool     `json:"email_enabled"`
	SMTPHost     string   `json:"smtp_host"`
	SMTPPort     int      `json:"smtp_port"`
	SMTPUser     string   `json:"smtp_user"`
	SMTPPassword string   `json:"smtp_password"`
	EmailFrom    string   `json:"email_from"`
	EmailTo      []string `json:"email_to"`

	// Webhook配置
	WebhookEnabled bool     `json:"webhook_enabled"`
	WebhookURLs    []string `json:"webhook_urls"`

	// 微信/钉钉配置
	DingTalkEnabled bool   `json:"dingtalk_enabled"`
	DingTalkWebhook string `json:"dingtalk_webhook"`

	WeChatEnabled bool   `json:"wechat_enabled"`
	WeChatWebhook string `json:"wechat_webhook"`

	// 报警阈值
	DrawdownThreshold    float64 `json:"drawdown_threshold"`     // 回撤阈值
	DailyLossThreshold   float64 `json:"daily_loss_threshold"`   // 日亏损阈值
	RiskLimitThreshold   float64 `json:"risk_limit_threshold"`   // 风险限额阈值
	MarginRatioThreshold float64 `json:"margin_ratio_threshold"` // 保证金比例阈值

	// 报警冷却时间(秒)
	CooldownSeconds int `json:"cooldown_seconds"`

	// 开关
	Enabled bool `json:"enabled"`
}

// DefaultAlertConfig 默认报警配置
func DefaultAlertConfig() *AlertConfig {
	return &AlertConfig{
		DrawdownThreshold:    0.10, // 10%回撤报警
		DailyLossThreshold:   0.05, // 5%日亏损报警
		RiskLimitThreshold:   0.80, // 80%风险限额使用率报警
		MarginRatioThreshold: 1.3,  // 130%保证金比例报警
		CooldownSeconds:      300,  // 5分钟冷却
		Enabled:              true,
	}
}

// AlertManager 报警管理器
type AlertManager struct {
	config       *AlertConfig
	alertHistory []*Alert
	alertCount   map[AlertType]int
	cooldownMap  map[string]time.Time
	collector    *MonitorCollector

	mu sync.RWMutex
}

// NewAlertManager 创建报警管理器
func NewAlertManager(config *AlertConfig, collector *MonitorCollector) *AlertManager {
	if config == nil {
		config = DefaultAlertConfig()
	}
	return &AlertManager{
		config:       config,
		alertHistory: make([]*Alert, 0, 1000),
		alertCount:   make(map[AlertType]int),
		cooldownMap:  make(map[string]time.Time),
		collector:    collector,
	}
}

// CheckAlerts 检查报警条件
func (am *AlertManager) CheckAlerts(data *DashboardData) {
	if !am.config.Enabled {
		return
	}

	// 检查回撤报警
	am.checkDrawdown(data.Returns)

	// 检查风险报警
	am.checkRisk(data.Risk)

	// 检查系统状态
	am.checkSystem(data.System)

	// 检查训练状态
	am.checkTraining(data.Training)
}

// checkDrawdown 检查回撤报警
func (am *AlertManager) checkDrawdown(returns *ReturnMetrics) {
	if returns == nil {
		return
	}

	// 最大回撤报警
	if returns.MaxDrawdown >= am.config.DrawdownThreshold {
		am.triggerAlert(&Alert{
			Type:  AlertDrawdown,
			Level: LevelWarning,
			Title: "最大回撤报警",
			Message: fmt.Sprintf("当前最大回撤 %.2f%% 超过阈值 %.2f%%",
				returns.MaxDrawdown*100, am.config.DrawdownThreshold*100),
			Value:     returns.MaxDrawdown,
			Threshold: am.config.DrawdownThreshold,
			Channels:  []string{"email", "webhook"},
		})
	}

	// 日亏损报警
	if returns.DailyReturn < -am.config.DailyLossThreshold {
		am.triggerAlert(&Alert{
			Type:  AlertLoss,
			Level: LevelCritical,
			Title: "日亏损报警",
			Message: fmt.Sprintf("当日亏损 %.2f%% 超过阈值 %.2f%%",
				-returns.DailyReturn*100, am.config.DailyLossThreshold*100),
			Value:     -returns.DailyReturn,
			Threshold: am.config.DailyLossThreshold,
			Channels:  []string{"email", "webhook", "dingtalk"},
		})
	}
}

// checkRisk 检查风险报警
func (am *AlertManager) checkRisk(risk *RiskMetrics) {
	if risk == nil {
		return
	}

	// 风险限额使用率报警
	if risk.RiskLimitUsage >= am.config.RiskLimitThreshold {
		am.triggerAlert(&Alert{
			Type:  AlertRisk,
			Level: LevelWarning,
			Title: "风险限额报警",
			Message: fmt.Sprintf("风险限额使用率 %.2f%% 接近上限",
				risk.RiskLimitUsage*100),
			Value:     risk.RiskLimitUsage,
			Threshold: am.config.RiskLimitThreshold,
			Channels:  []string{"email", "webhook"},
		})
	}

	// 保证金比例报警
	if risk.MarginRatio >= am.config.MarginRatioThreshold {
		am.triggerAlert(&Alert{
			Type:      AlertMargin,
			Level:     LevelCritical,
			Title:     "保证金比例报警",
			Message:   fmt.Sprintf("保证金比例 %.2f 接近强平线", risk.MarginRatio),
			Value:     risk.MarginRatio,
			Threshold: am.config.MarginRatioThreshold,
			Channels:  []string{"email", "webhook", "dingtalk", "wechat"},
		})
	}
}

// checkSystem 检查系统状态
func (am *AlertManager) checkSystem(system *SystemStatus) {
	if system == nil {
		return
	}

	if system.Status == "error" {
		am.triggerAlert(&Alert{
			Type:     AlertSystem,
			Level:    LevelEmergency,
			Title:    "系统异常报警",
			Message:  "交易系统运行异常,请立即检查",
			Channels: []string{"email", "webhook", "dingtalk", "wechat"},
		})
	}

	if system.DataFeedStatus == "disconnected" {
		am.triggerAlert(&Alert{
			Type:     AlertDataFeed,
			Level:    LevelWarning,
			Title:    "数据源异常报警",
			Message:  "数据源连接已断开",
			Channels: []string{"email", "webhook"},
		})
	}
}

// checkTraining 检查训练状态
func (am *AlertManager) checkTraining(training *TrainingMetrics) {
	if training == nil {
		return
	}

	if training.Status == "failed" {
		am.triggerAlert(&Alert{
			Type:     AlertModelError,
			Level:    LevelCritical,
			Title:    "模型训练失败",
			Message:  fmt.Sprintf("模型训练失败: Epoch %d", training.CurrentEpoch),
			Channels: []string{"email", "webhook"},
		})
	}
}

// triggerAlert 触发报警
func (am *AlertManager) triggerAlert(alert *Alert) {
	alert.ID = fmt.Sprintf("%s_%d", alert.Type, time.Now().Unix())
	alert.Timestamp = time.Now().Format("2006-01-02 15:04:05")

	// 检查冷却时间
	if !am.checkCooldown(alert.Type) {
		return
	}

	// 记录报警
	am.mu.Lock()
	am.alertHistory = append(am.alertHistory, alert)
	am.alertCount[alert.Type]++
	am.cooldownMap[string(alert.Type)] = time.Now()
	am.mu.Unlock()

	// 发送报警
	log.Printf("[ALERT] %s: %s", alert.Title, alert.Message)

	// 并发发送到各通道
	go func() {
		for _, channel := range alert.Channels {
			switch channel {
			case "email":
				am.sendEmail(alert)
			case "webhook":
				am.sendWebhook(alert)
			case "dingtalk":
				am.sendDingTalk(alert)
			case "wechat":
				am.sendWeChat(alert)
			}
		}
	}()
}

// checkCooldown 检查冷却时间
func (am *AlertManager) checkCooldown(alertType AlertType) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	lastTime, exists := am.cooldownMap[string(alertType)]
	if !exists {
		return true
	}

	cooldown := time.Duration(am.config.CooldownSeconds) * time.Second
	return time.Since(lastTime) > cooldown
}

// sendEmail 发送邮件
func (am *AlertManager) sendEmail(alert *Alert) {
	if !am.config.EmailEnabled {
		return
	}

	subject := fmt.Sprintf("[量化交易报警] %s", alert.Title)
	body := fmt.Sprintf(`
类型: %s
级别: %s
时间: %s

%s

详细信息:
- 当前值: %.4f
- 阈值: %.4f
- 标的: %s

---
此邮件由量化交易监控系统自动发送
`, alert.Type, alert.Level, alert.Timestamp, alert.Message,
		alert.Value, alert.Threshold, alert.Symbol)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		am.config.EmailFrom,
		strings.Join(am.config.EmailTo, ","),
		subject,
		body,
	)

	auth := smtp.PlainAuth("", am.config.SMTPUser, am.config.SMTPPassword, am.config.SMTPHost)
	addr := fmt.Sprintf("%s:%d", am.config.SMTPHost, am.config.SMTPPort)

	err := smtp.SendMail(addr, auth, am.config.EmailFrom, am.config.EmailTo, []byte(msg))
	if err != nil {
		log.Printf("[ALERT] 邮件发送失败: %v", err)
	} else {
		log.Printf("[ALERT] 邮件发送成功: %s", alert.Title)
	}
}

// sendWebhook 发送Webhook
func (am *AlertManager) sendWebhook(alert *Alert) {
	if !am.config.WebhookEnabled || len(am.config.WebhookURLs) == 0 {
		return
	}

	payload := map[string]interface{}{
		"id":        alert.ID,
		"type":      alert.Type,
		"level":     alert.Level,
		"title":     alert.Title,
		"message":   alert.Message,
		"timestamp": alert.Timestamp,
		"value":     alert.Value,
		"threshold": alert.Threshold,
	}

	jsonData, _ := json.Marshal(payload)

	for _, url := range am.config.WebhookURLs {
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("[ALERT] Webhook发送失败 %s: %v", url, err)
			continue
		}
		resp.Body.Close()
		log.Printf("[ALERT] Webhook发送成功: %s", url)
	}
}

// sendDingTalk 发送钉钉通知
func (am *AlertManager) sendDingTalk(alert *Alert) {
	if !am.config.DingTalkEnabled || am.config.DingTalkWebhook == "" {
		return
	}

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("[报警] %s", alert.Title),
			"text": fmt.Sprintf("### %s\n\n**级别**: %s\n\n**类型**: %s\n\n**时间**: %s\n\n**详情**: %s\n\n**当前值**: %.4f\n\n**阈值**: %.4f",
				alert.Title, alert.Level, alert.Type, alert.Timestamp,
				alert.Message, alert.Value, alert.Threshold),
		},
	}

	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post(am.config.DingTalkWebhook, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[ALERT] 钉钉发送失败: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[ALERT] 钉钉发送成功")
}

// sendWeChat 发送企业微信通知
func (am *AlertManager) sendWeChat(alert *Alert) {
	if !am.config.WeChatEnabled || am.config.WeChatWebhook == "" {
		return
	}

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": fmt.Sprintf("### %s\n>级别: %s\n>类型: %s\n>时间: %s\n\n%s",
				alert.Title, alert.Level, alert.Type, alert.Timestamp, alert.Message),
		},
	}

	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post(am.config.WeChatWebhook, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[ALERT] 企业微信发送失败: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[ALERT] 企业微信发送成功")
}

// GetAlertHistory 获取报警历史
func (am *AlertManager) GetAlertHistory(limit int) []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if limit <= 0 || limit > len(am.alertHistory) {
		limit = len(am.alertHistory)
	}

	start := len(am.alertHistory) - limit
	if start < 0 {
		start = 0
	}

	result := make([]*Alert, limit)
	copy(result, am.alertHistory[start:])
	return result
}

// GetAlertStats 获取报警统计
func (am *AlertManager) GetAlertStats() map[AlertType]int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make(map[AlertType]int)
	for k, v := range am.alertCount {
		result[k] = v
	}
	return result
}

// AcknowledgeAlert 确认报警
func (am *AlertManager) AcknowledgeAlert(alertID string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	for _, alert := range am.alertHistory {
		if alert.ID == alertID {
			alert.Acknowledged = true
			return nil
		}
	}

	return fmt.Errorf("alert not found: %s", alertID)
}

// CustomAlert 自定义报警
func (am *AlertManager) CustomAlert(alertType AlertType, level AlertLevel, title, message string, channels []string) {
	am.triggerAlert(&Alert{
		Type:     alertType,
		Level:    level,
		Title:    title,
		Message:  message,
		Channels: channels,
	})
}
