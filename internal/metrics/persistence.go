package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"proxy-go/internal/models"
	"proxy-go/internal/utils"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsStorage 指标存储结构
type MetricsStorage struct {
	collector        *Collector
	saveInterval     time.Duration
	dataDir          string
	stopChan         chan struct{}
	wg               sync.WaitGroup
	lastSaveTime     time.Time
	mutex            sync.RWMutex
	metricsFile      string
	pathStatsFile    string
	statusCodeFile   string
	refererStatsFile string
}

// NewMetricsStorage 创建新的指标存储
func NewMetricsStorage(collector *Collector, dataDir string, saveInterval time.Duration) *MetricsStorage {
	if saveInterval < time.Minute {
		saveInterval = time.Minute
	}

	return &MetricsStorage{
		collector:        collector,
		saveInterval:     saveInterval,
		dataDir:          dataDir,
		stopChan:         make(chan struct{}),
		metricsFile:      filepath.Join(dataDir, "metrics.json"),
		pathStatsFile:    filepath.Join(dataDir, "path_stats.json"),
		statusCodeFile:   filepath.Join(dataDir, "status_codes.json"),
		refererStatsFile: filepath.Join(dataDir, "referer_stats.json"),
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

	// 保存基本指标 - 只保存必要的字段
	basicMetrics := map[string]interface{}{
		"uptime":            stats["uptime"],
		"total_bytes":       stats["total_bytes"],
		"avg_response_time": stats["avg_response_time"],
		"save_time":         time.Now().Format(time.RFC3339),
	}

	// 单独保存延迟统计，避免嵌套结构导致的内存占用
	if latencyStats, ok := stats["latency_stats"].(map[string]interface{}); ok {
		basicMetrics["latency_min"] = latencyStats["min"]
		basicMetrics["latency_max"] = latencyStats["max"]
	}

	if err := saveJSONToFile(ms.metricsFile, basicMetrics); err != nil {
		return fmt.Errorf("保存基本指标失败: %v", err)
	}

	// 保存路径统计 - 限制数量
	topPaths := stats["top_paths"]
	if err := saveJSONToFile(ms.pathStatsFile, topPaths); err != nil {
		return fmt.Errorf("保存路径统计失败: %v", err)
	}

	// 保存状态码统计
	if err := saveJSONToFile(ms.statusCodeFile, stats["status_code_stats"]); err != nil {
		return fmt.Errorf("保存状态码统计失败: %v", err)
	}

	// 保存引用来源统计 - 限制数量
	topReferers := stats["top_referers"]
	if err := saveJSONToFile(ms.refererStatsFile, topReferers); err != nil {
		return fmt.Errorf("保存引用来源统计失败: %v", err)
	}

	// 单独保存延迟分布
	if latencyStats, ok := stats["latency_stats"].(map[string]interface{}); ok {
		if distribution, ok := latencyStats["distribution"]; ok {
			if err := saveJSONToFile(filepath.Join(ms.dataDir, "latency_distribution.json"), distribution); err != nil {
				log.Printf("[MetricsStorage] 保存延迟分布失败: %v", err)
			}
		}
	}

	ms.mutex.Lock()
	ms.lastSaveTime = time.Now()
	ms.mutex.Unlock()

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

	// 检查文件是否存在
	if !fileExists(ms.metricsFile) {
		return fmt.Errorf("指标数据文件不存在")
	}

	// 加载基本指标
	var basicMetrics map[string]interface{}
	if err := loadJSONFromFile(ms.metricsFile, &basicMetrics); err != nil {
		return fmt.Errorf("加载基本指标失败: %v", err)
	}

	// 将加载的数据应用到收集器
	// 1. 应用总字节数
	if totalBytes, ok := basicMetrics["total_bytes"].(float64); ok {
		atomic.StoreInt64(&ms.collector.totalBytes, int64(totalBytes))
	}

	// 2. 加载路径统计（如果文件存在）
	if fileExists(ms.pathStatsFile) {
		var pathStats []map[string]interface{}
		if err := loadJSONFromFile(ms.pathStatsFile, &pathStats); err != nil {
			log.Printf("[MetricsStorage] 加载路径统计失败: %v", err)
		} else {
			// 只加载前10个路径统计
			maxPaths := 10
			if len(pathStats) > maxPaths {
				pathStats = pathStats[:maxPaths]
			}

			for _, pathStat := range pathStats {
				path, ok := pathStat["path"].(string)
				if !ok {
					continue
				}

				requestCount, _ := pathStat["request_count"].(float64)
				errorCount, _ := pathStat["error_count"].(float64)
				bytesTransferred, _ := pathStat["bytes_transferred"].(float64)

				// 创建或更新路径统计
				var pathMetrics *models.PathMetrics
				if existingMetrics, ok := ms.collector.pathStats.Load(path); ok {
					pathMetrics = existingMetrics.(*models.PathMetrics)
				} else {
					pathMetrics = &models.PathMetrics{Path: path}
					ms.collector.pathStats.Store(path, pathMetrics)
				}

				// 设置统计值
				pathMetrics.RequestCount.Store(int64(requestCount))
				pathMetrics.ErrorCount.Store(int64(errorCount))
				pathMetrics.BytesTransferred.Store(int64(bytesTransferred))
			}
			log.Printf("[MetricsStorage] 加载了 %d 条路径统计", len(pathStats))
		}
	}

	// 3. 加载状态码统计（如果文件存在）
	if fileExists(ms.statusCodeFile) {
		var statusCodeStats map[string]interface{}
		if err := loadJSONFromFile(ms.statusCodeFile, &statusCodeStats); err != nil {
			log.Printf("[MetricsStorage] 加载状态码统计失败: %v", err)
		} else {
			for statusCode, count := range statusCodeStats {
				countValue, ok := count.(float64)
				if !ok {
					continue
				}

				// 创建或更新状态码统计
				if counter, ok := ms.collector.statusCodeStats.Load(statusCode); ok {
					atomic.StoreInt64(counter.(*int64), int64(countValue))
				} else {
					counter := new(int64)
					*counter = int64(countValue)
					ms.collector.statusCodeStats.Store(statusCode, counter)
				}
			}
			log.Printf("[MetricsStorage] 加载了 %d 条状态码统计", len(statusCodeStats))
		}
	}

	// 4. 加载引用来源统计（如果文件存在）
	if fileExists(ms.refererStatsFile) {
		var refererStats []map[string]interface{}
		if err := loadJSONFromFile(ms.refererStatsFile, &refererStats); err != nil {
			log.Printf("[MetricsStorage] 加载引用来源统计失败: %v", err)
		} else {
			// 只加载前10个引用来源统计
			maxReferers := 10
			if len(refererStats) > maxReferers {
				refererStats = refererStats[:maxReferers]
			}

			for _, refererStat := range refererStats {
				referer, ok := refererStat["path"].(string)
				if !ok {
					continue
				}

				requestCount, _ := refererStat["request_count"].(float64)
				errorCount, _ := refererStat["error_count"].(float64)
				bytesTransferred, _ := refererStat["bytes_transferred"].(float64)

				// 创建或更新引用来源统计
				var refererMetrics *models.PathMetrics
				if existingMetrics, ok := ms.collector.refererStats.Load(referer); ok {
					refererMetrics = existingMetrics.(*models.PathMetrics)
				} else {
					refererMetrics = &models.PathMetrics{Path: referer}
					ms.collector.refererStats.Store(referer, refererMetrics)
				}

				// 设置统计值
				refererMetrics.RequestCount.Store(int64(requestCount))
				refererMetrics.ErrorCount.Store(int64(errorCount))
				refererMetrics.BytesTransferred.Store(int64(bytesTransferred))
			}
			log.Printf("[MetricsStorage] 加载了 %d 条引用来源统计", len(refererStats))
		}
	}

	// 5. 加载延迟分布（如果文件存在）
	latencyDistributionFile := filepath.Join(ms.dataDir, "latency_distribution.json")
	if fileExists(latencyDistributionFile) {
		var distribution map[string]interface{}
		if err := loadJSONFromFile(latencyDistributionFile, &distribution); err != nil {
			log.Printf("[MetricsStorage] 加载延迟分布失败: %v", err)
		} else {
			for bucket, count := range distribution {
				countValue, ok := count.(float64)
				if !ok {
					continue
				}

				if counter, ok := ms.collector.latencyBuckets.Load(bucket); ok {
					atomic.StoreInt64(counter.(*int64), int64(countValue))
				}
			}
			log.Printf("[MetricsStorage] 加载了延迟分布数据")
		}
	}

	ms.mutex.Lock()
	if saveTime, ok := basicMetrics["save_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, saveTime); err == nil {
			ms.lastSaveTime = t
		}
	}
	ms.mutex.Unlock()

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
