package metrics

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// NewTestCollector creates a new collector for testing
func NewTestCollector() *Collector {
	return &Collector{
		startTime:      time.Now(),
		pathStats:      sync.Map{},
		statusStats:    [6]atomic.Int64{},
		latencyBuckets: [10]atomic.Int64{},
	}
}

func TestDataConsistency(t *testing.T) {
	c := NewTestCollector()

	// 测试基础指标
	c.RecordRequest("/test", 200, time.Second, 1024, "127.0.0.1", nil)
	if err := c.CheckDataConsistency(); err != nil {
		t.Errorf("Data consistency check failed: %v", err)
	}

	// 测试错误数据
	c.persistentStats.totalErrors.Store(100)
	c.persistentStats.totalRequests.Store(50)
	if err := c.CheckDataConsistency(); err == nil {
		t.Error("Expected error for invalid data")
	}
}
