package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"proxy-go/internal/service"
)

// MetricsHandler 指标处理器
type MetricsHandler struct {
	metricsService *service.MetricsService
}

// NewMetricsHandler 创建新的指标处理器
func NewMetricsHandler(metricsService *service.MetricsService) *MetricsHandler {
	return &MetricsHandler{
		metricsService: metricsService,
	}
}

// GetMetrics 处理获取指标请求
func (h *MetricsHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metricsData := h.metricsService.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metricsData); err != nil {
		log.Printf("Error encoding metrics: %v", err)
	}
}
