package monitor

import (
	"fmt"
	"log"
	"proxy-go/internal/constants"
	"sync"
	"sync/atomic"
	"time"
)

type AlertLevel string

const (
	AlertLevelError AlertLevel = "ERROR"
	AlertLevelWarn  AlertLevel = "WARN"
	AlertLevelInfo  AlertLevel = "INFO"
)

type Alert struct {
	Level   AlertLevel
	Message string
	Time    time.Time
}

type AlertHandler interface {
	HandleAlert(alert Alert)
}

// 日志告警处理器
type LogAlertHandler struct {
	logger *log.Logger
}

type ErrorStats struct {
	totalRequests atomic.Int64
	errorRequests atomic.Int64
	timestamp     time.Time
}

type TransferStats struct {
	bytes     atomic.Int64
	duration  atomic.Int64
	timestamp time.Time
}

type Monitor struct {
	alerts         chan Alert
	handlers       []AlertHandler
	dedup          sync.Map
	lastNotify     sync.Map
	errorWindow    [12]ErrorStats
	currentWindow  atomic.Int32
	transferWindow [12]TransferStats
	currentTWindow atomic.Int32
}

func NewMonitor() *Monitor {
	m := &Monitor{
		alerts:   make(chan Alert, 100),
		handlers: make([]AlertHandler, 0),
	}

	// 初始化第一个窗口
	m.errorWindow[0] = ErrorStats{timestamp: time.Now()}
	m.transferWindow[0] = TransferStats{timestamp: time.Now()}

	// 添加默认的日志处理器
	m.AddHandler(&LogAlertHandler{
		logger: log.New(log.Writer(), "[ALERT] ", log.LstdFlags),
	})

	// 启动告警处理
	go m.processAlerts()

	// 启动窗口清理
	go m.cleanupWindows()

	return m
}

func (m *Monitor) AddHandler(handler AlertHandler) {
	m.handlers = append(m.handlers, handler)
}

func (m *Monitor) processAlerts() {
	for alert := range m.alerts {
		// 检查是否在去重时间窗口内
		key := fmt.Sprintf("%s:%s", alert.Level, alert.Message)
		if _, ok := m.dedup.LoadOrStore(key, time.Now()); ok {
			continue
		}

		// 检查是否在通知间隔内
		notifyKey := fmt.Sprintf("notify:%s", alert.Level)
		if lastTime, ok := m.lastNotify.Load(notifyKey); ok {
			if time.Since(lastTime.(time.Time)) < constants.AlertNotifyInterval {
				continue
			}
		}
		m.lastNotify.Store(notifyKey, time.Now())

		for _, handler := range m.handlers {
			handler.HandleAlert(alert)
		}
	}
}

func (m *Monitor) CheckMetrics(stats map[string]interface{}) {
	currentIdx := int(m.currentWindow.Load())
	window := &m.errorWindow[currentIdx]

	if time.Since(window.timestamp) >= constants.AlertWindowInterval {
		// 轮转到下一个窗口
		nextIdx := (currentIdx + 1) % constants.AlertWindowSize
		m.errorWindow[nextIdx] = ErrorStats{timestamp: time.Now()}
		m.currentWindow.Store(int32(nextIdx))
	}

	var recentErrors, recentRequests int64
	now := time.Now()
	for i := 0; i < constants.AlertWindowSize; i++ {
		idx := (currentIdx - i + constants.AlertWindowSize) % constants.AlertWindowSize
		w := &m.errorWindow[idx]

		if now.Sub(w.timestamp) <= constants.AlertDedupeWindow {
			recentErrors += w.errorRequests.Load()
			recentRequests += w.totalRequests.Load()
		}
	}

	// 检查错误率
	if recentRequests >= constants.MinRequestsForAlert {
		errorRate := float64(recentErrors) / float64(recentRequests)
		if errorRate > constants.ErrorRateThreshold {
			m.alerts <- Alert{
				Level: AlertLevelError,
				Message: fmt.Sprintf("最近%d分钟内错误率过高: %.2f%% (错误请求: %d, 总请求: %d)",
					int(constants.AlertDedupeWindow.Minutes()),
					errorRate*100, recentErrors, recentRequests),
				Time: time.Now(),
			}
		}
	}
}

func (m *Monitor) CheckLatency(latency time.Duration, bytes int64) {
	// 更新传输速率窗口
	currentIdx := int(m.currentTWindow.Load())
	window := &m.transferWindow[currentIdx]

	if time.Since(window.timestamp) >= constants.AlertWindowInterval {
		// 轮转到下一个窗口
		nextIdx := (currentIdx + 1) % constants.AlertWindowSize
		m.transferWindow[nextIdx] = TransferStats{timestamp: time.Now()}
		m.currentTWindow.Store(int32(nextIdx))
		currentIdx = nextIdx
		window = &m.transferWindow[currentIdx]
	}

	window.bytes.Add(bytes)
	window.duration.Add(int64(latency))

	// 计算最近15分钟的平均传输速率
	var totalBytes, totalDuration int64
	now := time.Now()
	for i := 0; i < constants.AlertWindowSize; i++ {
		idx := (currentIdx - i + constants.AlertWindowSize) % constants.AlertWindowSize
		w := &m.transferWindow[idx]

		if now.Sub(w.timestamp) <= constants.AlertDedupeWindow {
			totalBytes += w.bytes.Load()

			totalDuration += w.duration.Load()
		}
	}

	if totalDuration > 0 {
		avgRate := float64(totalBytes) / (float64(totalDuration) / float64(time.Second))

		// 根据文件大小计算最小速率要求
		var (
			fileSize   int64
			maxLatency time.Duration
		)
		switch {
		case bytes < constants.SmallFileSize:
			fileSize = constants.SmallFileSize
			maxLatency = constants.SmallFileLatency
		case bytes < constants.MediumFileSize:
			fileSize = constants.MediumFileSize
			maxLatency = constants.MediumFileLatency
		case bytes < constants.LargeFileSize:
			fileSize = constants.LargeFileSize
			maxLatency = constants.LargeFileLatency
		default:
			fileSize = bytes
			maxLatency = constants.HugeFileLatency
		}

		// 计算最小速率 = 文件大小 / 最大允许延迟
		minRate := float64(fileSize) / maxLatency.Seconds()

		// 只有当15分钟内的平均传输速率低于阈值时才告警
		if avgRate < minRate {
			m.alerts <- Alert{
				Level: AlertLevelWarn,
				Message: fmt.Sprintf(
					"最近%d分钟内平均传输速率过低: %.2f MB/s (最低要求: %.2f MB/s, 基准文件大小: %s, 最大延迟: %s)",
					int(constants.AlertDedupeWindow.Minutes()),
					avgRate/float64(constants.MB),
					minRate/float64(constants.MB),
					formatBytes(fileSize),
					maxLatency,
				),
				Time: time.Now(),
			}
		}
	}
}

// 日志处理器实现
func (h *LogAlertHandler) HandleAlert(alert Alert) {
	h.logger.Printf("[%s] %s", alert.Level, alert.Message)
}

func (m *Monitor) RecordRequest() {
	currentIdx := int(m.currentWindow.Load())
	window := &m.errorWindow[currentIdx]
	window.totalRequests.Add(1)
}

func (m *Monitor) RecordError() {
	currentIdx := int(m.currentWindow.Load())
	window := &m.errorWindow[currentIdx]
	window.errorRequests.Add(1)
}

// 格式化字节大小
func formatBytes(bytes int64) string {
	switch {
	case bytes >= constants.MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(constants.MB))
	case bytes >= constants.KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(constants.KB))
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}

// 添加窗口清理
func (m *Monitor) cleanupWindows() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		now := time.Now()
		// 清理过期的去重记录
		m.dedup.Range(func(key, value interface{}) bool {
			if timestamp, ok := value.(time.Time); ok {
				if now.Sub(timestamp) > constants.AlertDedupeWindow {
					m.dedup.Delete(key)
				}
			}
			return true
		})
	}
}
