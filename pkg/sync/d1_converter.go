package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ============================================
// Path Stats 转换
// ============================================

// ConvertPathStatsFromFile 从 path_stats.json 文件转换为 PathStat 数组
func ConvertPathStatsFromFile(filePath string) ([]PathStat, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var fileData struct {
		PathStats  map[string]PathStatOld `json:"path_stats"`
		LastUpdate string                  `json:"last_update"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	stats := make([]PathStat, 0, len(fileData.PathStats))
	now := time.Now().UnixMilli()

	for path, oldStat := range fileData.PathStats {
		stats = append(stats, PathStat{
			Path:             path,
			RequestCount:     oldStat.RequestCount,
			ErrorCount:       oldStat.ErrorCount,
			BytesTransferred: oldStat.BytesTransferred,
			Status2xx:        oldStat.Status2xx,
			Status3xx:        oldStat.Status3xx,
			Status4xx:        oldStat.Status4xx,
			Status5xx:        oldStat.Status5xx,
			CacheHits:        oldStat.CacheHits,
			CacheMisses:      oldStat.CacheMisses,
			CacheHitRate:     oldStat.CacheHitRate,
			BytesSaved:       oldStat.BytesSaved,
			AvgLatency:       oldStat.AvgLatency,
			LastAccessTime:   oldStat.LastAccessTime,
			UpdatedAt:        now,
		})
	}

	return stats, nil
}

// PathStatOld 旧的 JSON 格式
type PathStatOld struct {
	Path             string  `json:"path"`
	RequestCount     int64   `json:"request_count"`
	ErrorCount       int64   `json:"error_count"`
	BytesTransferred int64   `json:"bytes_transferred"`
	Status2xx        int64   `json:"status_2xx"`
	Status3xx        int64   `json:"status_3xx"`
	Status4xx        int64   `json:"status_4xx"`
	Status5xx        int64   `json:"status_5xx"`
	CacheHits        int64   `json:"cache_hits"`
	CacheMisses      int64   `json:"cache_misses"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	BytesSaved       int64   `json:"bytes_saved"`
	AvgLatency       string  `json:"avg_latency"`
	LastAccessTime   int64   `json:"last_access_time"`
}

// ============================================
// Banned IPs 转换
// ============================================

// ConvertBannedIPsFromFile 从 banned_ips.json 文件转换为 BannedIP 数组
func ConvertBannedIPsFromFile(filePath string) ([]BannedIP, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var fileData struct {
		ActiveBans map[string]BannedIPOld   `json:"active_bans"`
		History    []BannedIPHistoryOld     `json:"history"`
		LastUpdate string                    `json:"last_update"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	bans := make([]BannedIP, 0, len(fileData.ActiveBans))
	now := time.Now().UnixMilli()

	// 转换当前封禁
	for _, oldBan := range fileData.ActiveBans {
		bans = append(bans, BannedIP{
			IP:          oldBan.IP,
			BanTime:     oldBan.BanTime.UnixMilli(),
			BanEndTime:  oldBan.BanEndTime.UnixMilli(),
			Reason:      oldBan.Reason,
			ErrorCount:  oldBan.ErrorCount,
			IsActive:    oldBan.IsActive,
			UnbanTime:   convertTimePtr(oldBan.UnbanTime),
			UnbanReason: oldBan.UnbanReason,
			UpdatedAt:   now,
		})
	}

	return bans, nil
}

// ConvertBannedIPHistoryFromFile 从 banned_ips.json 文件转换历史记录
func ConvertBannedIPHistoryFromFile(filePath string) ([]BannedIPHistory, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var fileData struct {
		ActiveBans map[string]BannedIPOld   `json:"active_bans"`
		History    []BannedIPHistoryOld     `json:"history"`
		LastUpdate string                    `json:"last_update"`
	}

	if err := json.Unmarshal(data, &fileData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	history := make([]BannedIPHistory, 0, len(fileData.History))
	now := time.Now().UnixMilli()

	// 转换历史记录
	for _, oldHistory := range fileData.History {
		history = append(history, BannedIPHistory{
			IP:          oldHistory.IP,
			BanTime:     oldHistory.BanTime.UnixMilli(),
			BanEndTime:  oldHistory.BanEndTime.UnixMilli(),
			Reason:      oldHistory.Reason,
			ErrorCount:  oldHistory.ErrorCount,
			UnbanTime:   convertTimePtr(oldHistory.UnbanTime),
			UnbanReason: oldHistory.UnbanReason,
			CreatedAt:   now,
		})
	}

	return history, nil
}

// BannedIPOld 旧的 JSON 格式
type BannedIPOld struct {
	IP          string     `json:"ip"`
	BanTime     time.Time  `json:"ban_time"`
	BanEndTime  time.Time  `json:"ban_end_time"`
	Reason      string     `json:"reason"`
	ErrorCount  int        `json:"error_count"`
	IsActive    bool       `json:"is_active"`
	UnbanTime   *time.Time `json:"unban_time,omitempty"`
	UnbanReason string     `json:"unban_reason,omitempty"`
}

// BannedIPHistoryOld 旧的历史记录格式
type BannedIPHistoryOld struct {
	IP          string     `json:"ip"`
	BanTime     time.Time  `json:"ban_time"`
	BanEndTime  time.Time  `json:"ban_end_time"`
	Reason      string     `json:"reason"`
	ErrorCount  int        `json:"error_count"`
	UnbanTime   *time.Time `json:"unban_time,omitempty"`
	UnbanReason string     `json:"unban_reason,omitempty"`
}

// ============================================
// Config 转换
// ============================================

// ConvertConfigFromFile 从 config.json 文件转换为 ConfigMap 和 ConfigOther
func ConvertConfigFromFile(filePath string) ([]ConfigMap, []ConfigOther, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	now := time.Now().UnixMilli()
	var maps []ConfigMap
	var others []ConfigOther

	// 转换 MAP (路径配置)
	if mapData, ok := config["MAP"].(map[string]any); ok {
		for path, value := range mapData {
			mapConfig, ok := value.(map[string]any)
			if !ok {
				continue
			}

			// 提取基本字段
			defaultTarget, _ := mapConfig["DefaultTarget"].(string)
			enabled, _ := mapConfig["Enabled"].(bool)

			// 转换 ExtensionMap 为 JSON 字符串
			var extensionRules string
			if extMap, ok := mapConfig["ExtensionMap"].(map[string]any); ok && len(extMap) > 0 {
				extJSON, _ := json.Marshal(extMap)
				extensionRules = string(extJSON)
			}

			// 转换 CacheConfig 为 JSON 字符串
			var cacheConfig string
			if cc, ok := mapConfig["CacheConfig"].(map[string]any); ok && len(cc) > 0 {
				ccJSON, _ := json.Marshal(cc)
				cacheConfig = string(ccJSON)
			}

			maps = append(maps, ConfigMap{
				Path:           path,
				DefaultTarget:  defaultTarget,
				Enabled:        enabled,
				ExtensionRules: extensionRules,
				CacheConfig:    cacheConfig,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
	}

	// 转换其他配置
	for key, value := range config {
		if key == "MAP" {
			continue // MAP 已经单独处理
		}

		valueJSON, err := json.Marshal(value)
		if err != nil {
			continue
		}

		others = append(others, ConfigOther{
			Key:       key,
			Value:     string(valueJSON),
			UpdatedAt: now,
		})
	}

	return maps, others, nil
}

// ============================================
// 辅助函数
// ============================================

func convertTimePtr(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}
