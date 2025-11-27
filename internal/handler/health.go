package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"proxy-go/internal/service"
	"time"
)

// HealthHandler 健康检查管理接口
type HealthHandler struct {
	proxyService *service.ProxyService
}

// NewHealthHandler 创建健康检查处理器
func NewHealthHandler(proxyService *service.ProxyService) *HealthHandler {
	return &HealthHandler{
		proxyService: proxyService,
	}
}

// HealthStatusResponse 健康状态响应
type HealthStatusResponse struct {
	Targets []TargetHealthStatus `json:"targets"`
	Summary HealthSummary        `json:"summary"`
}

// TargetHealthStatus 目标健康状态
type TargetHealthStatus struct {
	URL            string  `json:"url"`
	IsHealthy      bool    `json:"is_healthy"`
	LastCheck      string  `json:"last_check"`
	LastSuccess    string  `json:"last_success"`
	FailCount      int     `json:"fail_count"`
	SuccessCount   int     `json:"success_count"`
	TotalRequests  int64   `json:"total_requests"`
	FailedRequests int64   `json:"failed_requests"`
	SuccessRate    float64 `json:"success_rate"`
	AvgLatency     string  `json:"avg_latency"`
	LastError      string  `json:"last_error,omitempty"`
}

// HealthSummary 健康摘要
type HealthSummary struct {
	TotalTargets     int     `json:"total_targets"`
	HealthyTargets   int     `json:"healthy_targets"`
	UnhealthyTargets int     `json:"unhealthy_targets"`
	OverallHealth    float64 `json:"overall_health"`
}

// GetHealthStatus 获取所有目标的健康状态
func (h *HealthHandler) GetHealthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	healthChecker := h.proxyService.GetHealthChecker()
	if healthChecker == nil {
		http.Error(w, "Health checker not enabled", http.StatusServiceUnavailable)
		return
	}

	allHealth := healthChecker.GetAllHealth()
	targets := make([]TargetHealthStatus, 0, len(allHealth))
	healthyCount := 0
	totalCount := 0

	for url, health := range allHealth {
		totalCount++
		if health.IsHealthy {
			healthyCount++
		}

		successRate := float64(0)
		if health.TotalRequests > 0 {
			successRate = float64(health.TotalRequests-health.FailedRequests) / float64(health.TotalRequests) * 100
		}

		targets = append(targets, TargetHealthStatus{
			URL:            url,
			IsHealthy:      health.IsHealthy,
			LastCheck:      formatTime(health.LastCheck),
			LastSuccess:    formatTime(health.LastSuccess),
			FailCount:      health.FailCount,
			SuccessCount:   health.SuccessCount,
			TotalRequests:  health.TotalRequests,
			FailedRequests: health.FailedRequests,
			SuccessRate:    successRate,
			AvgLatency:     health.AvgLatency.String(),
			LastError:      health.LastError,
		})
	}

	overallHealth := float64(0)
	if totalCount > 0 {
		overallHealth = float64(healthyCount) / float64(totalCount) * 100
	}

	response := HealthStatusResponse{
		Targets: targets,
		Summary: HealthSummary{
			TotalTargets:     totalCount,
			HealthyTargets:   healthyCount,
			UnhealthyTargets: totalCount - healthyCount,
			OverallHealth:    overallHealth,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("[Health API] Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// ResetTargetHealth 重置目标健康状态
func (h *HealthHandler) ResetTargetHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	healthChecker := h.proxyService.GetHealthChecker()
	if healthChecker == nil {
		http.Error(w, "Health checker not enabled", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	healthChecker.ResetTarget(req.URL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Target health reset successfully",
		"url":     req.URL,
	})
}

// ClearAllHealth 清理所有健康检查记录
func (h *HealthHandler) ClearAllHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	healthChecker := h.proxyService.GetHealthChecker()
	if healthChecker == nil {
		http.Error(w, "Health checker not enabled", http.StatusServiceUnavailable)
		return
	}

	count := healthChecker.ClearAllTargets()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "All health records cleared successfully",
		"count":   count,
	})
}

// formatTime 格式化时间
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04:05")
}
