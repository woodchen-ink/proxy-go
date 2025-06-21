package security

import (
	"log"
	"sync"
	"time"
)

// IPBanManager IP封禁管理器
type IPBanManager struct {
	// 404错误计数器 map[ip]count
	errorCounts sync.Map
	// IP封禁列表 map[ip]banEndTime
	bannedIPs sync.Map
	// 配置参数
	config *IPBanConfig
	// 清理任务停止信号
	stopCleanup chan struct{}
	// 清理任务等待组
	cleanupWG sync.WaitGroup
}

// IPBanConfig IP封禁配置
type IPBanConfig struct {
	// 404错误阈值，超过此数量将被封禁
	ErrorThreshold int `json:"error_threshold"`
	// 统计窗口时间（分钟）
	WindowMinutes int `json:"window_minutes"`
	// 封禁时长（分钟）
	BanDurationMinutes int `json:"ban_duration_minutes"`
	// 清理间隔（分钟）
	CleanupIntervalMinutes int `json:"cleanup_interval_minutes"`
}

// errorRecord 错误记录
type errorRecord struct {
	count     int
	firstTime time.Time
	lastTime  time.Time
}

// DefaultIPBanConfig 默认配置
func DefaultIPBanConfig() *IPBanConfig {
	return &IPBanConfig{
		ErrorThreshold:         10, // 10次404错误
		WindowMinutes:          5,  // 5分钟内
		BanDurationMinutes:     5,  // 封禁5分钟
		CleanupIntervalMinutes: 1,  // 每分钟清理一次
	}
}

// NewIPBanManager 创建IP封禁管理器
func NewIPBanManager(config *IPBanConfig) *IPBanManager {
	if config == nil {
		config = DefaultIPBanConfig()
	}

	manager := &IPBanManager{
		config:      config,
		stopCleanup: make(chan struct{}),
	}

	// 启动清理任务
	manager.startCleanupTask()

	log.Printf("[Security] IP封禁管理器已启动 - 阈值: %d次/%.0f分钟, 封禁时长: %.0f分钟",
		config.ErrorThreshold,
		float64(config.WindowMinutes),
		float64(config.BanDurationMinutes))

	return manager
}

// RecordError 记录404错误
func (m *IPBanManager) RecordError(ip string) {
	now := time.Now()
	windowStart := now.Add(-time.Duration(m.config.WindowMinutes) * time.Minute)

	// 加载或创建错误记录
	value, _ := m.errorCounts.LoadOrStore(ip, &errorRecord{
		count:     0,
		firstTime: now,
		lastTime:  now,
	})
	record := value.(*errorRecord)

	// 如果第一次记录时间超出窗口，重置计数
	if record.firstTime.Before(windowStart) {
		record.count = 1
		record.firstTime = now
		record.lastTime = now
	} else {
		record.count++
		record.lastTime = now
	}

	// 检查是否需要封禁
	if record.count >= m.config.ErrorThreshold {
		m.banIP(ip, now)
		// 重置计数器，避免重复封禁
		record.count = 0
		record.firstTime = now
	}

	log.Printf("[Security] 记录404错误 IP: %s, 当前计数: %d/%d (窗口: %.0f分钟)",
		ip, record.count, m.config.ErrorThreshold, float64(m.config.WindowMinutes))
}

// banIP 封禁IP
func (m *IPBanManager) banIP(ip string, banTime time.Time) {
	banEndTime := banTime.Add(time.Duration(m.config.BanDurationMinutes) * time.Minute)
	m.bannedIPs.Store(ip, banEndTime)

	log.Printf("[Security] IP已被封禁: %s, 封禁至: %s (%.0f分钟)",
		ip, banEndTime.Format("15:04:05"), float64(m.config.BanDurationMinutes))
}

// IsIPBanned 检查IP是否被封禁
func (m *IPBanManager) IsIPBanned(ip string) bool {
	value, exists := m.bannedIPs.Load(ip)
	if !exists {
		return false
	}

	banEndTime := value.(time.Time)
	now := time.Now()

	// 检查封禁是否已过期
	if now.After(banEndTime) {
		m.bannedIPs.Delete(ip)
		log.Printf("[Security] IP封禁已过期，自动解封: %s", ip)
		return false
	}

	return true
}

// GetBanInfo 获取IP封禁信息
func (m *IPBanManager) GetBanInfo(ip string) (bool, time.Time) {
	value, exists := m.bannedIPs.Load(ip)
	if !exists {
		return false, time.Time{}
	}

	banEndTime := value.(time.Time)
	now := time.Now()

	if now.After(banEndTime) {
		m.bannedIPs.Delete(ip)
		return false, time.Time{}
	}

	return true, banEndTime
}

// UnbanIP 手动解封IP
func (m *IPBanManager) UnbanIP(ip string) bool {
	_, exists := m.bannedIPs.Load(ip)
	if exists {
		m.bannedIPs.Delete(ip)
		log.Printf("[Security] 手动解封IP: %s", ip)
		return true
	}
	return false
}

// GetBannedIPs 获取所有被封禁的IP列表
func (m *IPBanManager) GetBannedIPs() map[string]time.Time {
	result := make(map[string]time.Time)
	now := time.Now()

	m.bannedIPs.Range(func(key, value interface{}) bool {
		ip := key.(string)
		banEndTime := value.(time.Time)

		// 清理过期的封禁
		if now.After(banEndTime) {
			m.bannedIPs.Delete(ip)
		} else {
			result[ip] = banEndTime
		}
		return true
	})

	return result
}

// GetStats 获取统计信息
func (m *IPBanManager) GetStats() map[string]interface{} {
	bannedCount := 0
	errorRecordCount := 0

	m.bannedIPs.Range(func(key, value interface{}) bool {
		bannedCount++
		return true
	})

	m.errorCounts.Range(func(key, value interface{}) bool {
		errorRecordCount++
		return true
	})

	return map[string]interface{}{
		"banned_ips_count":    bannedCount,
		"error_records_count": errorRecordCount,
		"config":              m.config,
	}
}

// startCleanupTask 启动清理任务
func (m *IPBanManager) startCleanupTask() {
	m.cleanupWG.Add(1)
	go func() {
		defer m.cleanupWG.Done()
		ticker := time.NewTicker(time.Duration(m.config.CleanupIntervalMinutes) * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.cleanup()
			case <-m.stopCleanup:
				return
			}
		}
	}()
}

// cleanup 清理过期数据
func (m *IPBanManager) cleanup() {
	now := time.Now()
	windowStart := now.Add(-time.Duration(m.config.WindowMinutes) * time.Minute)

	// 清理过期的错误记录
	var expiredIPs []string
	m.errorCounts.Range(func(key, value interface{}) bool {
		ip := key.(string)
		record := value.(*errorRecord)

		// 如果最后一次错误时间超出窗口，删除记录
		if record.lastTime.Before(windowStart) {
			expiredIPs = append(expiredIPs, ip)
		}
		return true
	})

	for _, ip := range expiredIPs {
		m.errorCounts.Delete(ip)
	}

	// 清理过期的封禁记录
	var expiredBans []string
	m.bannedIPs.Range(func(key, value interface{}) bool {
		ip := key.(string)
		banEndTime := value.(time.Time)

		if now.After(banEndTime) {
			expiredBans = append(expiredBans, ip)
		}
		return true
	})

	for _, ip := range expiredBans {
		m.bannedIPs.Delete(ip)
	}

	if len(expiredIPs) > 0 || len(expiredBans) > 0 {
		log.Printf("[Security] 清理任务完成 - 清理错误记录: %d, 清理过期封禁: %d",
			len(expiredIPs), len(expiredBans))
	}
}

// Stop 停止IP封禁管理器
func (m *IPBanManager) Stop() {
	close(m.stopCleanup)
	m.cleanupWG.Wait()
	log.Printf("[Security] IP封禁管理器已停止")
}
