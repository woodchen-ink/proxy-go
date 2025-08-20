package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/security"
	"proxy-go/internal/service"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// SecurityHandler 安全管理处理器
type SecurityHandler struct {
	securityService *service.SecurityService
}

// NewSecurityHandler 创建安全管理处理器
func NewSecurityHandler(banManager *security.IPBanManager) *SecurityHandler {
	return &SecurityHandler{
		securityService: service.NewSecurityService(banManager),
	}
}

// GetBannedIPs 获取被封禁的IP列表
func (sh *SecurityHandler) GetBannedIPs(w http.ResponseWriter, r *http.Request) {
	result, err := sh.securityService.GetBannedIPs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// UnbanIP 手动解封IP
func (sh *SecurityHandler) UnbanIP(w http.ResponseWriter, r *http.Request) {
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

	result, err := sh.securityService.UnbanIP(req.IP)
	if err != nil {
		if err.Error() == "IP address is required" {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetSecurityStats 获取安全统计信息
func (sh *SecurityHandler) GetSecurityStats(w http.ResponseWriter, r *http.Request) {
	stats, err := sh.securityService.GetSecurityStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// CheckIPStatus 检查IP状态
func (sh *SecurityHandler) CheckIPStatus(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	fallbackIP := iputil.GetClientIP(r)

	result, err := sh.securityService.CheckIPStatus(ip, fallbackIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
