package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"proxy-go/internal/utils"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsStorage 指标存储结构
type MetricsStorage struct {
	collector      *Collector
	saveInterval   time.Duration
	dataDir        string
	stopChan       chan struct{}
	wg             sync.WaitGroup
	lastSaveTime   time.Time
	mutex          sync.RWMutex
	statusCodeFile string
}

// NewMetricsStorage 创建新的指标存储
func NewMetricsStorage(collector *Collector, dataDir string, saveInterval time.Duration) *MetricsStorage {
	if saveInterval < time.Minute {
		saveInterval = time.Minute
	}

	return &MetricsStorage{
		collector:      collector,
		saveInterval:   saveInterval,
		dataDir:        dataDir,
		stopChan:       make(chan struct{}),
		statusCodeFile: filepath.Join(dataDir, "status_codes.json"),
	}
}

// Start 启动定时保存任务
func (ms *MetricsStorage) Start() error {
	// 确保数据目录存在
	if err := os.MkdirAll(ms.dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	// 尝试加载现有数据
	if err := ms.LoadMetrics(); err != nil {
		log.Printf("[MetricsStorage] 加载指标数据失败: %v", err)
		// 加载失败不影响启动
	}

	ms.wg.Add(1)
	go ms.runSaveTask()
	log.Printf("[MetricsStorage] 指标存储服务已启动，保存间隔: %v", ms.saveInterval)
	return nil
}

// Stop 停止定时保存任务
func (ms *MetricsStorage) Stop() {
	close(ms.stopChan)
	ms.wg.Wait()

	// 在停止前保存一次数据
	if err := ms.SaveMetrics(); err != nil {
		log.Printf("[MetricsStorage] 停止时保存指标数据失败: %v", err)
	}

	log.Printf("[MetricsStorage] 指标存储服务已停止")
}

// runSaveTask 运行定时保存任务
func (ms *MetricsStorage) runSaveTask() {
	defer ms.wg.Done()

	ticker := time.NewTicker(ms.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := ms.SaveMetrics(); err != nil {
				log.Printf("[MetricsStorage] 保存指标数据失败: %v", err)
			}
		case <-ms.stopChan:
			return
		}
	}
}

// SaveMetrics 保存指标数据
func (ms *MetricsStorage) SaveMetrics() error {
	start := time.Now()
	log.Printf("[MetricsStorage] 开始保存指标数据...")

	// 获取当前指标数据
	stats := ms.collector.GetStats()

	// 保存状态码统计
	if err := saveJSONToFile(ms.statusCodeFile, stats["status_code_stats"]); err != nil {
		return fmt.Errorf("保存状态码统计失败: %v", err)
	}

	// 不再保存引用来源统计，因为它现在只保存在内存中

	// 单独保存延迟分布
	if latencyStats, ok := stats["latency_stats"].(map[string]interface{}); ok {
		if distribution, ok := latencyStats["distribution"]; ok {
			if err := saveJSONToFile(filepath.Join(ms.dataDir, "latency_distribution.json"), distribution); err != nil {
				log.Printf("[MetricsStorage] 保存延迟分布失败: %v", err)
			}
		}
	}

	// 强制进行一次GC
	runtime.GC()

	// 打印内存使用情况
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf("[MetricsStorage] 指标数据保存完成，耗时: %v, 内存使用: %s",
		time.Since(start), utils.FormatBytes(int64(mem.Alloc)))
	return nil
}

// LoadMetrics 加载指标数据
func (ms *MetricsStorage) LoadMetrics() error {
	start := time.Now()
	log.Printf("[MetricsStorage] 开始加载指标数据...")

	// 不再加载 basicMetrics（metrics.json）

	// 1. 加载状态码统计（如果文件存在）
	if fileExists(ms.statusCodeFile) {
		var statusCodeStats map[string]interface{}
		if err := loadJSONFromFile(ms.statusCodeFile, &statusCodeStats); err != nil {
			log.Printf("[MetricsStorage] 加载状态码统计失败: %v", err)
		} else {
			// 由于新的 StatusCodeStats 结构，我们需要手动设置值
			loadedCount := 0
			for codeStr, countValue := range statusCodeStats {
				// 解析状态码
				if code, err := strconv.Atoi(codeStr); err == nil {
					// 解析计数值
					var count int64
					switch v := countValue.(type) {
					case float64:
						count = int64(v)
					case int64:
						count = v
					case int:
						count = int64(v)
					default:
						continue
					}

					// 手动设置到新的 StatusCodeStats 结构中
					ms.collector.statusCodeStats.mu.Lock()
					if _, exists := ms.collector.statusCodeStats.stats[code]; !exists {
						ms.collector.statusCodeStats.stats[code] = new(int64)
					}
					atomic.StoreInt64(ms.collector.statusCodeStats.stats[code], count)
					ms.collector.statusCodeStats.mu.Unlock()
					loadedCount++
				}
			}
			log.Printf("[MetricsStorage] 成功加载了 %d 条状态码统计", loadedCount)
		}
	}

	// 不再加载引用来源统计，因为它现在只保存在内存中

	// 3. 加载延迟分布（如果文件存在）
	latencyDistributionFile := filepath.Join(ms.dataDir, "latency_distribution.json")
	if fileExists(latencyDistributionFile) {
		var distribution map[string]interface{}
		if err := loadJSONFromFile(latencyDistributionFile, &distribution); err != nil {
			log.Printf("[MetricsStorage] 加载延迟分布失败: %v", err)
		} else {
			// 由于新的 LatencyBuckets 结构，我们需要手动设置值
			for bucket, count := range distribution {
				countValue, ok := count.(float64)
				if !ok {
					continue
				}

				// 根据桶名称设置对应的值
				switch bucket {
				case "lt10ms":
					atomic.StoreInt64(&ms.collector.latencyBuckets.lt10ms, int64(countValue))
				case "10-50ms":
					atomic.StoreInt64(&ms.collector.latencyBuckets.ms10_50, int64(countValue))
				case "50-200ms":
					atomic.StoreInt64(&ms.collector.latencyBuckets.ms50_200, int64(countValue))
				case "200-1000ms":
					atomic.StoreInt64(&ms.collector.latencyBuckets.ms200_1000, int64(countValue))
				case "gt1s":
					atomic.StoreInt64(&ms.collector.latencyBuckets.gt1s, int64(countValue))
				}
			}
			log.Printf("[MetricsStorage] 加载了延迟分布数据")
		}
	}
	// 强制进行一次GC
	runtime.GC()

	// 打印内存使用情况
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf("[MetricsStorage] 指标数据加载完成，耗时: %v, 内存使用: %s",
		time.Since(start), utils.FormatBytes(int64(mem.Alloc)))
	return nil
}

// GetLastSaveTime 获取最后保存时间
func (ms *MetricsStorage) GetLastSaveTime() time.Time {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.lastSaveTime
}

// 辅助函数：保存JSON到文件
func saveJSONToFile(filename string, data interface{}) error {
	// 创建临时文件
	tempFile := filename + ".tmp"

	// 将数据编码为JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	// 写入临时文件
	if err := os.WriteFile(tempFile, jsonData, 0644); err != nil {
		return err
	}

	// 重命名临时文件为目标文件（原子操作）
	return os.Rename(tempFile, filename)
}

// 辅助函数：从文件加载JSON
func loadJSONFromFile(filename string, data interface{}) error {
	// 读取文件内容
	jsonData, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// 解码JSON数据
	return json.Unmarshal(jsonData, data)
}

// 辅助函数：检查文件是否存在
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
