package metrics

import (
	"sync/atomic"
	"time"
)

// RequestLog 记录单个请求的信息
type RequestLog struct {
	Time      time.Time
	Path      string
	Status    int
	Latency   time.Duration
	BytesSent int64
	ClientIP  string
}

// PathStats 记录路径统计信息
type PathStats struct {
	Requests   atomic.Int64
	Errors     atomic.Int64
	Bytes      atomic.Int64
	LatencySum atomic.Int64
}
