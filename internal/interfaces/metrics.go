package interfaces

import (
	"time"
)

// MetricsCollector 定义指标收集器接口
type MetricsCollector interface {
	CheckDataConsistency() error
	GetLastSaveTime() time.Time
}
