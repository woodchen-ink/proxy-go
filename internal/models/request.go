package models

import (
	"sync"
	"time"
)

// RequestLog 请求日志
type RequestLog struct {
	Time      time.Time `json:"time"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	Latency   int64     `json:"latency"`
	BytesSent int64     `json:"bytes_sent"`
	ClientIP  string    `json:"client_ip"`
}

// RequestQueue 请求队列
type RequestQueue struct {
	sync.RWMutex
	items  []RequestLog
	size   int
	cursor int
}

// NewRequestQueue 创建新的请求队列
func NewRequestQueue(size int) *RequestQueue {
	return &RequestQueue{
		items: make([]RequestLog, size),
		size:  size,
	}
}

// Push 添加请求日志
func (q *RequestQueue) Push(log RequestLog) {
	q.Lock()
	defer q.Unlock()
	q.items[q.cursor] = log
	q.cursor = (q.cursor + 1) % q.size
}

// GetAll 获取所有请求日志
func (q *RequestQueue) GetAll() []RequestLog {
	q.RLock()
	defer q.RUnlock()
	result := make([]RequestLog, 0, q.size)
	for i := 0; i < q.size; i++ {
		idx := (q.cursor - i - 1 + q.size) % q.size
		if !q.items[idx].Time.IsZero() {
			result = append(result, q.items[idx])
		}
	}
	return result
}
