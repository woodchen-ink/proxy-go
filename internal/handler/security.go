package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/security"
	"proxy-go/internal/utils"
	"time"
)

// SecurityHandler 安全管理处理器
type SecurityHandler struct {
	banManager *security.IPBanManager
}

// NewSecurityHandler 创建安全管理处理器
func NewSecurityHandler(banManager *security.IPBanManager) *SecurityHandler {
	return &SecurityHandler{
		banManager: banManager,
	}
}

// GetBannedIPs 获取被封禁的IP列表
func (sh *SecurityHandler) GetBannedIPs(w http.ResponseWriter, r *http.Request) {
	if sh.banManager == nil {
		http.Error(w, "Security manager not enabled", http.StatusServiceUnavailable)
		return
	}

	bannedIPs := sh.banManager.GetBannedIPs()

	// 转换为前端友好的格式
	result := make([]map[string]interface{}, 0, len(bannedIPs))
	for ip, banEndTime := range bannedIPs {
		result = append(result, map[string]interface{}{
			"ip":                ip,
			"ban_end_time":      banEndTime.Format("2006-01-02 15:04:05"),
			"remaining_seconds": int64(time.Until(banEndTime).Seconds()),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"banned_ips": result,
		"count":      len(result),
	})
}

// UnbanIP 手动解封IP
func (sh *SecurityHandler) UnbanIP(w http.ResponseWriter, r *http.Request) {
	if sh.banManager == nil {
		http.Error(w, "Security manager not enabled", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IP string `json:"ip"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.IP == "" {
		http.Error(w, "IP address is required", http.StatusBadRequest)
		return
	}

	success := sh.banManager.UnbanIP(req.IP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": success,
		"message": func() string {
			if success {
				return "IP解封成功"
			}
			return "IP未在封禁列表中"
		}(),
	})
}

// GetSecurityStats 获取安全统计信息
func (sh *SecurityHandler) GetSecurityStats(w http.ResponseWriter, r *http.Request) {
	if sh.banManager == nil {
		http.Error(w, "Security manager not enabled", http.StatusServiceUnavailable)
		return
	}

	stats := sh.banManager.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// CheckIPStatus 检查IP状态
func (sh *SecurityHandler) CheckIPStatus(w http.ResponseWriter, r *http.Request) {
	if sh.banManager == nil {
		http.Error(w, "Security manager not enabled", http.StatusServiceUnavailable)
		return
	}

	ip := r.URL.Query().Get("ip")
	if ip == "" {
		// 如果没有指定IP，使用请求的IP
		ip = utils.GetClientIP(r)
	}

	banned, banEndTime := sh.banManager.GetBanInfo(ip)

	result := map[string]interface{}{
		"ip":     ip,
		"banned": banned,
	}

	if banned {
		result["ban_end_time"] = banEndTime.Format("2006-01-02 15:04:05")
		result["remaining_seconds"] = int64(time.Until(banEndTime).Seconds())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
