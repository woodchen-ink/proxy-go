package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/metrics"
)

type PathStatsHandler struct {
	collector *metrics.Collector
}

func NewPathStatsHandler(collector *metrics.Collector) *PathStatsHandler {
	return &PathStatsHandler{
		collector: collector,
	}
}

// GetAllPathStats 获取所有路径的统计信息
func (h *PathStatsHandler) GetAllPathStats(w http.ResponseWriter, r *http.Request) {
	stats := h.collector.GetPathStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path_stats": stats,
	})
}
