package models

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"proxy-go/internal/constants"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type RequestLog struct {
	Time      time.Time
	Path      string
	Status    int
	Latency   time.Duration
	BytesSent int64
	ClientIP  string
}

type PathStats struct {
	Requests   atomic.Int64
	Errors     atomic.Int64
	Bytes      atomic.Int64
	LatencySum atomic.Int64
}

type HistoricalMetrics struct {
	Timestamp     string  `json:"timestamp"`
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
	TotalBytes    int64   `json:"total_bytes"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
}

type PathMetrics struct {
	Path             string `json:"path"`
	RequestCount     int64  `json:"request_count"`
	ErrorCount       int64  `json:"error_count"`
	AvgLatency       string `json:"avg_latency"`
	BytesTransferred int64  `json:"bytes_transferred"`
}

type MetricsDB struct {
	DB    *sql.DB
	stats struct {
		queries     atomic.Int64
		slowQueries atomic.Int64
		errors      atomic.Int64
		lastError   atomic.Value // string
	}
}

type PerformanceMetrics struct {
	Timestamp         string  `json:"timestamp"`
	AvgResponseTime   int64   `json:"avg_response_time"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	BytesPerSecond    float64 `json:"bytes_per_second"`
}

func NewMetricsDB(dbPath string) (*MetricsDB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 设置连接池参数
	db.SetMaxOpenConns(1) // SQLite 只支持一个写连接
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// 设置数据库优化参数
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %v", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("failed to set journal_mode: %v", err)
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		return nil, fmt.Errorf("failed to set synchronous: %v", err)
	}
	if _, err := db.Exec("PRAGMA cache_size = -2000"); err != nil {
		return nil, fmt.Errorf("failed to set cache_size: %v", err)
	}
	if _, err := db.Exec("PRAGMA temp_store = MEMORY"); err != nil {
		return nil, fmt.Errorf("failed to set temp_store: %v", err)
	}

	// 创建必要的表
	if err := initTables(db); err != nil {
		db.Close()
		return nil, err
	}

	mdb := &MetricsDB{DB: db}
	mdb.stats.lastError.Store("")
	return mdb, nil
}

func initTables(db *sql.DB) error {
	// 创指标历史表
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			total_requests INTEGER,
			total_errors INTEGER,
			total_bytes INTEGER,
			avg_latency INTEGER,
			error_rate REAL
		)
	`)
	if err != nil {
		return err
	}

	// 创建状态码统计表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS status_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			status_group TEXT,
			count INTEGER
		)
	`)
	if err != nil {
		return err
	}

	// 创建路径统计表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS path_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			path TEXT,
			requests INTEGER,
			errors INTEGER,
			bytes INTEGER,
			avg_latency INTEGER
		)
	`)
	if err != nil {
		return err
	}

	// 添加索引以提高查询性能
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics_history(timestamp);
		CREATE INDEX IF NOT EXISTS idx_status_timestamp ON status_stats(timestamp);
		CREATE INDEX IF NOT EXISTS idx_path_timestamp ON path_stats(timestamp);

		-- 复合引，用于优化聚合查询
		CREATE INDEX IF NOT EXISTS idx_metrics_timestamp_values ON metrics_history(
			timestamp,
			total_requests,
			total_errors,
			total_bytes,
			avg_latency
		);

		-- 路径统计的复合索引
		CREATE INDEX IF NOT EXISTS idx_path_stats_composite ON path_stats(
			timestamp,
			path,
			requests
		);

		-- 状态码统计的复合索引
		CREATE INDEX IF NOT EXISTS idx_status_stats_composite ON status_stats(
			timestamp,
			status_group,
			count
		);
	`)
	if err != nil {
		return err
	}

	// 添加新的表
	if _, err := db.Exec(`
		-- 性能指标表
		CREATE TABLE IF NOT EXISTS performance_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			avg_response_time INTEGER,
			requests_per_second REAL,
			bytes_per_second REAL
		);

		-- 状态码统计历史表
		CREATE TABLE IF NOT EXISTS status_code_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			status_group TEXT,
			count INTEGER
		);

		-- 热门路径历史表
		CREATE TABLE IF NOT EXISTS popular_paths_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			path TEXT,
			request_count INTEGER,
			error_count INTEGER,
			avg_latency TEXT,
			bytes_transferred INTEGER
		);

		-- 引用来源历史表
		CREATE TABLE IF NOT EXISTS referer_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			referer TEXT,
			request_count INTEGER
		);

		-- 为新表添加索引
		CREATE INDEX IF NOT EXISTS idx_performance_timestamp ON performance_metrics(timestamp);
		CREATE INDEX IF NOT EXISTS idx_status_code_history_timestamp ON status_code_history(timestamp);
		CREATE INDEX IF NOT EXISTS idx_popular_paths_history_timestamp ON popular_paths_history(timestamp);
		CREATE INDEX IF NOT EXISTS idx_referer_history_timestamp ON referer_history(timestamp);
	`); err != nil {
		return err
	}

	// 添加新的索引
	if _, err := db.Exec(`
		-- 为性能指标表添加复合索引
		CREATE INDEX IF NOT EXISTS idx_performance_metrics_composite ON performance_metrics(
			timestamp,
			avg_response_time,
			requests_per_second,
			bytes_per_second
		);

		-- 为状态码历史表添加复合索引
		CREATE INDEX IF NOT EXISTS idx_status_code_history_composite ON status_code_history(
			timestamp,
			status_group,
			count
		);

		-- 为热门路径历史表添加复合索引
		CREATE INDEX IF NOT EXISTS idx_popular_paths_history_composite ON popular_paths_history(
			timestamp,
			path,
			request_count
		);
	`); err != nil {
		return err
	}

	// 启动定期清理任务
	go cleanupRoutine(db)

	return nil
}

// 定期清理旧数据
func cleanupRoutine(db *sql.DB) {
	// 避免在启动时就立即清理
	time.Sleep(5 * time.Minute)

	ticker := time.NewTicker(constants.CleanupInterval)
	for range ticker.C {
		start := time.Now()
		var totalDeleted int64

		// 检查数据库大小
		var dbSize int64
		row := db.QueryRow("SELECT page_count * page_size FROM pragma_page_count, pragma_page_size")
		if err := row.Scan(&dbSize); err != nil {
			log.Printf("Error getting database size: %v", err)
			continue
		}
		log.Printf("Current database size: %s", FormatBytes(uint64(dbSize)))

		tx, err := db.Begin()
		if err != nil {
			log.Printf("Error starting cleanup transaction: %v", err)
			continue
		}

		// 优化理性能
		if _, err := tx.Exec("PRAGMA synchronous = NORMAL"); err != nil {
			log.Printf("Error setting synchronous mode: %v", err)
		}
		if _, err := tx.Exec("PRAGMA journal_mode = WAL"); err != nil {
			log.Printf("Error setting journal mode: %v", err)
		}
		if _, err := tx.Exec("PRAGMA temp_store = MEMORY"); err != nil {
			log.Printf("Error setting temp store: %v", err)
		}
		if _, err := tx.Exec("PRAGMA cache_size = -2000"); err != nil {
			log.Printf("Error setting cache size: %v", err)
		}

		// 先清理索引
		if _, err := tx.Exec("ANALYZE"); err != nil {
			log.Printf("Error running ANALYZE: %v", err)
		}
		if _, err := tx.Exec("PRAGMA optimize"); err != nil {
			log.Printf("Error running optimize: %v", err)
		}

		// 使用不同的保留时间清理不同类型的数据
		cleanupTables := []struct {
			table     string
			retention time.Duration
		}{
			{"metrics_history", constants.MetricsRetention},
			{"performance_metrics", constants.MetricsRetention},
			{"status_code_history", constants.StatusRetention},
			{"status_stats", constants.StatusRetention},
			{"popular_paths_history", constants.PathRetention},
			{"path_stats", constants.PathRetention},
			{"referer_history", constants.RefererRetention},
		}

		for _, t := range cleanupTables {
			cutoff := time.Now().Add(-t.retention)
			// 使用批删除提高性能
			for {
				result, err := tx.Exec(`DELETE FROM `+t.table+` WHERE timestamp < ? LIMIT 1000`, cutoff)
				if err != nil {
					tx.Rollback()
					log.Printf("Error cleaning %s: %v", t.table, err)
					break
				}
				rows, _ := result.RowsAffected()
				totalDeleted += rows
				if rows < 1000 {
					break
				}
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Error committing cleanup transaction: %v", err)
		} else {
			newSize := getDBSize(db)
			if newSize == 0 {
				log.Printf("Cleaned up %d old records in %v, but failed to get new DB size",
					totalDeleted, time.Since(start))
			} else {
				log.Printf("Cleaned up %d old records in %v, freed %s",
					totalDeleted, time.Since(start),
					FormatBytes(uint64(dbSize-newSize)))
			}
		}
	}
}

// 获取数据库大小
func getDBSize(db *sql.DB) int64 {
	var size int64
	row := db.QueryRow("SELECT page_count * page_size FROM pragma_page_count, pragma_page_size")
	if err := row.Scan(&size); err != nil {
		log.Printf("Error getting database size: %v", err)
		return 0
	}
	return size
}

func (db *MetricsDB) SaveMetrics(stats map[string]interface{}) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 保存基础指标
	stmt, err := tx.Prepare(`
		INSERT INTO metrics_history (
			total_requests, total_errors, total_bytes, avg_latency, error_rate
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	// 类型断言检查
	totalReqs, ok := stats["total_requests"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_requests type")
	}
	totalErrs, ok := stats["total_errors"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_errors type")
	}
	totalBytes, ok := stats["total_bytes"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_bytes type")
	}
	avgLatency, ok := stats["avg_latency"].(int64)
	if !ok {
		return fmt.Errorf("invalid avg_latency type")
	}

	// 计算错误率
	var errorRate float64
	if totalReqs > 0 {
		errorRate = float64(totalErrs) / float64(totalReqs)
	}

	// 保存基础指标
	_, err = stmt.Exec(
		totalReqs,
		totalErrs,
		totalBytes,
		avgLatency,
		errorRate,
	)
	if err != nil {
		return fmt.Errorf("failed to save metrics: %v", err)
	}

	// 保存状态码统计
	statusStats := stats["status_code_stats"].(map[string]int64)
	values := make([]string, 0, len(statusStats))
	args := make([]interface{}, 0, len(statusStats)*2)

	for group, count := range statusStats {
		values = append(values, "(?, ?)")
		args = append(args, group, count)
	}

	query := "INSERT INTO status_code_history (status_group, count) VALUES " +
		strings.Join(values, ",")

	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}

	// 保存热门路径
	pathStats := stats["top_paths"].([]PathMetrics)
	pathStmt, err := tx.Prepare(`
		INSERT INTO popular_paths_history (
			path, request_count, error_count, avg_latency, bytes_transferred
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()

	for _, p := range pathStats {
		if _, err := pathStmt.Exec(
			p.Path, p.RequestCount, p.ErrorCount,
			p.AvgLatency, p.BytesTransferred,
		); err != nil {
			return err
		}
	}

	// 保存引用来源
	refererStats := stats["top_referers"].([]PathMetrics)
	refererStmt, err := tx.Prepare(`
		INSERT INTO referer_history (referer, request_count)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer refererStmt.Close()

	for _, r := range refererStats {
		if _, err := refererStmt.Exec(r.Path, r.RequestCount); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *MetricsDB) Close() error {
	return db.DB.Close()
}

func (db *MetricsDB) GetRecentMetrics(hours float64) ([]HistoricalMetrics, error) {
	start := time.Now()
	var queryStats struct {
		rowsProcessed int
		cacheHits     int64
		cacheSize     int64
	}

	// 添加查询超时
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 处理小于1小时的情况
	if hours <= 0 {
		hours = 0.5 // 30分钟
	}

	// 计算合适的时间间隔
	var interval string
	var timeStep int
	switch {
	case hours <= 0.5: // 30分钟
		interval = "%Y-%m-%d %H:%M:00"
		timeStep = 1 // 1分钟
	case hours <= 1:
		interval = "%Y-%m-%d %H:%M:00"
		timeStep = 1 // 1分钟
	case hours <= 24:
		interval = "%Y-%m-%d %H:%M:00"
		timeStep = 5 // 5分钟
	case hours <= 168:
		interval = "%Y-%m-%d %H:00:00"
		timeStep = 60 // 1小时
	default:
		interval = "%Y-%m-%d 00:00:00"
		timeStep = 1440 // 1天
	}

	// 修改查询逻辑，优化性能
	rows, err := db.DB.QueryContext(ctx, `
		WITH RECURSIVE 
		time_points(ts) AS (
			SELECT strftime(?, datetime('now', 'localtime'))
			UNION ALL
			SELECT strftime(?, datetime(ts, '-' || ? || ' minutes'))
			FROM time_points
			WHERE ts > strftime(?, datetime('now', '-' || ? || ' hours', 'localtime'))
			LIMIT ?
		),
		base_metrics AS (
			-- 获取每个时间点的累计值
			SELECT 
				strftime(?, timestamp) as group_time,
				total_requests,
				total_errors,
				total_bytes,
				avg_latency
			FROM metrics_history
			WHERE timestamp >= datetime('now', '-' || ? || ' hours', 'localtime')
				AND timestamp < datetime('now', 'localtime')
		),
		grouped_metrics AS (
			-- 获取每个时间点的最大值
			SELECT 
				group_time,
				MAX(total_requests) as period_requests,
				MAX(total_errors) as period_errors,
				MAX(total_bytes) as period_bytes,
				AVG(avg_latency) as avg_latency
			FROM base_metrics
			GROUP BY group_time
		)
		SELECT 
			tp.ts as timestamp,
			-- 计算每个时间点的增量
			COALESCE(
				CASE 
					WHEN LAG(m.period_requests) OVER w IS NULL THEN m.period_requests
					ELSE m.period_requests - LAG(m.period_requests) OVER w
				END,
				0
			) as total_requests,
			COALESCE(
				CASE 
					WHEN LAG(m.period_errors) OVER w IS NULL THEN m.period_errors
					ELSE m.period_errors - LAG(m.period_errors) OVER w
				END,
				0
			) as total_errors,
			COALESCE(
				CASE 
					WHEN LAG(m.period_bytes) OVER w IS NULL THEN m.period_bytes
					ELSE m.period_bytes - LAG(m.period_bytes) OVER w
				END,
				0
			) as total_bytes,
			COALESCE(m.avg_latency, 0) as avg_latency
		FROM time_points tp
		LEFT JOIN grouped_metrics m ON tp.ts = m.group_time
		WINDOW w AS (ORDER BY tp.ts)
		ORDER BY timestamp DESC
		LIMIT ?
	`, interval, interval, timeStep, interval, hours, 1000, interval, hours, 1000)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []HistoricalMetrics
	for rows.Next() {
		var m HistoricalMetrics
		err := rows.Scan(
			&m.Timestamp,
			&m.TotalRequests,
			&m.TotalErrors,
			&m.TotalBytes,
			&m.AvgLatency,
		)
		if err != nil {
			return nil, err
		}

		// 确保数值非负
		if m.TotalRequests < 0 {
			m.TotalRequests = 0
		}
		if m.TotalErrors < 0 {
			m.TotalErrors = 0
		}
		if m.TotalBytes < 0 {
			m.TotalBytes = 0
		}

		// 计算错误率
		if m.TotalRequests > 0 {
			m.ErrorRate = float64(m.TotalErrors) / float64(m.TotalRequests)
		}
		metrics = append(metrics, m)
	}

	// 记录查询性能
	duration := time.Since(start)
	if duration > time.Second {
		log.Printf("Slow query warning: GetRecentMetrics(%v hours) took %v "+
			"(rows: %d, cache hits: %d, cache size: %s)",
			hours, duration, queryStats.rowsProcessed,
			queryStats.cacheHits, FormatBytes(uint64(queryStats.cacheSize)))
	}

	// 如果没有数据，返回一个空的记录
	if len(metrics) == 0 {
		now := time.Now()
		metrics = append(metrics, HistoricalMetrics{
			Timestamp:     now.Format("2006-01-02 15:04:05"),
			TotalRequests: 0,
			TotalErrors:   0,
			TotalBytes:    0,
			ErrorRate:     0,
			AvgLatency:    0,
		})
	}

	return metrics, rows.Err()
}

func (db *MetricsDB) SaveFullMetrics(stats map[string]interface{}) error {
	start := time.Now()
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 开始时记录数据库大小
	startSize := getDBSize(db.DB)

	// 优化写入性能
	if _, err := tx.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		return fmt.Errorf("failed to set synchronous mode: %v", err)
	}
	if _, err := tx.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("failed to set journal mode: %v", err)
	}
	if _, err := tx.Exec("PRAGMA temp_store = MEMORY"); err != nil {
		return fmt.Errorf("failed to set temp store: %v", err)
	}
	if _, err := tx.Exec("PRAGMA cache_size = -2000"); err != nil {
		return fmt.Errorf("failed to set cache size: %v", err)
	}

	// 使用事务提高写入性能
	if _, err := tx.Exec("PRAGMA synchronous = OFF"); err != nil {
		return fmt.Errorf("failed to set synchronous mode: %v", err)
	}
	if _, err := tx.Exec("PRAGMA journal_mode = MEMORY"); err != nil {
		return fmt.Errorf("failed to set journal mode: %v", err)
	}

	// 保存状态码统计
	statusStats := stats["status_code_stats"].(map[string]int64)
	values := make([]string, 0, len(statusStats))
	args := make([]interface{}, 0, len(statusStats)*2)

	for group, count := range statusStats {
		values = append(values, "(?, ?)")
		args = append(args, group, count)
	}

	query := "INSERT INTO status_code_history (status_group, count) VALUES " +
		strings.Join(values, ",")

	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}

	// 保存热门路径
	pathStats := stats["top_paths"].([]PathMetrics)
	pathStmt, err := tx.Prepare(`
		INSERT INTO popular_paths_history (
			path, request_count, error_count, avg_latency, bytes_transferred
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()

	for _, p := range pathStats {
		if _, err := pathStmt.Exec(
			p.Path, p.RequestCount, p.ErrorCount,
			p.AvgLatency, p.BytesTransferred,
		); err != nil {
			return err
		}
	}

	// 保存引用来源
	refererStats := stats["top_referers"].([]PathMetrics)
	refererStmt, err := tx.Prepare(`
		INSERT INTO referer_history (referer, request_count)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer refererStmt.Close()

	for _, r := range refererStats {
		if _, err := refererStmt.Exec(r.Path, r.RequestCount); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 记录写入的数据量和性能
	endSize := getDBSize(db.DB)
	duration := time.Since(start)
	log.Printf("Saved metrics: wrote %s to database in %v (%.2f MB/s)",
		FormatBytes(uint64(endSize-startSize)),
		duration,
		float64(endSize-startSize)/(1024*1024)/duration.Seconds(),
	)

	return nil
}

func (db *MetricsDB) GetLastMetrics() (*HistoricalMetrics, error) {
	row := db.DB.QueryRow(`
		SELECT 
			total_requests,
			total_errors,
			total_bytes,
			avg_latency
		FROM metrics_history 
		ORDER BY timestamp DESC 
		LIMIT 1
	`)

	var metrics HistoricalMetrics
	err := row.Scan(
		&metrics.TotalRequests,
		&metrics.TotalErrors,
		&metrics.TotalBytes,
		&metrics.AvgLatency,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &metrics, nil
}

func (db *MetricsDB) GetRecentPerformanceMetrics(hours int) ([]PerformanceMetrics, error) {
	rows, err := db.DB.Query(`
		SELECT 
			strftime('%Y-%m-%d %H:%M:00', timestamp, 'localtime') as ts,
			AVG(avg_response_time) as avg_response_time,
			AVG(requests_per_second) as requests_per_second,
			AVG(bytes_per_second) as bytes_per_second
		FROM performance_metrics
		WHERE timestamp >= datetime('now', '-' || ? || ' hours', 'localtime')
		GROUP BY ts
		ORDER BY ts DESC
	`, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []PerformanceMetrics
	for rows.Next() {
		var m PerformanceMetrics
		err := rows.Scan(
			&m.Timestamp,
			&m.AvgResponseTime,
			&m.RequestsPerSecond,
			&m.BytesPerSecond,
		)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}

// FormatBytes 格式化字节大小
func FormatBytes(bytes uint64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}

func (db *MetricsDB) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"total_queries": db.stats.queries.Load(),
		"slow_queries":  db.stats.slowQueries.Load(),
		"total_errors":  db.stats.errors.Load(),
		"last_error":    db.stats.lastError.Load(),
	}
}

func (db *MetricsDB) LoadRecentStats(statusStats *[6]atomic.Int64, pathStats *sync.Map, refererStats *sync.Map) error {
	// 加载状态码统计
	rows, err := db.DB.Query(`
		SELECT status_group, SUM(count) as count 
		FROM status_code_history 
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY status_group
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var group string
		var count int64
		if err := rows.Scan(&group, &count); err != nil {
			return err
		}
		if len(group) > 0 {
			idx := (int(group[0]) - '0') - 1
			if idx >= 0 && idx < len(statusStats) {
				statusStats[idx].Store(count)
			}
		}
	}

	// 加载路径统计
	rows, err = db.DB.Query(`
		SELECT 
			path, 
			SUM(request_count) as requests,
			SUM(error_count) as errors,
			AVG(bytes_transferred) as bytes,
			AVG(avg_latency) as latency
		FROM popular_paths_history
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY path
		ORDER BY requests DESC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		var requests, errors, bytes int64
		var latency float64
		if err := rows.Scan(&path, &requests, &errors, &bytes, &latency); err != nil {
			return err
		}
		stats := &PathStats{}
		stats.Requests.Store(requests)
		stats.Errors.Store(errors)
		stats.Bytes.Store(bytes)
		stats.LatencySum.Store(int64(latency))
		pathStats.Store(path, stats)
	}

	// 加载引用来源统计
	rows, err = db.DB.Query(`
		SELECT 
			referer,
			SUM(request_count) as requests
		FROM referer_history
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY referer
		ORDER BY requests DESC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var referer string
		var requests int64
		if err := rows.Scan(&referer, &requests); err != nil {
			return err
		}
		stats := &PathStats{}
		stats.Requests.Store(requests)
		refererStats.Store(referer, stats)
	}

	return nil
}

// SaveAllMetrics 合并所有指标的保存
func (db *MetricsDB) SaveAllMetrics(stats map[string]interface{}) error {
	start := time.Now()
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 保存基础指标
	stmt, err := tx.Prepare(`
		INSERT INTO metrics_history (
			total_requests, total_errors, total_bytes, avg_latency, error_rate
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	// 类型断言检查
	totalReqs, ok := stats["total_requests"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_requests type")
	}
	totalErrs, ok := stats["total_errors"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_errors type")
	}
	totalBytes, ok := stats["total_bytes"].(int64)
	if !ok {
		return fmt.Errorf("invalid total_bytes type")
	}
	avgLatency, ok := stats["avg_latency"].(int64)
	if !ok {
		return fmt.Errorf("invalid avg_latency type")
	}

	// 计算错误率
	var errorRate float64
	if totalReqs > 0 {
		errorRate = float64(totalErrs) / float64(totalReqs)
	}

	// 保存基础指标
	_, err = stmt.Exec(totalReqs, totalErrs, totalBytes, avgLatency, errorRate)
	if err != nil {
		return fmt.Errorf("failed to save basic metrics: %v", err)
	}

	// 保存状态码统计
	statusStats := stats["status_code_stats"].(map[string]int64)
	values := make([]string, 0, len(statusStats))
	args := make([]interface{}, 0, len(statusStats)*2)
	for group, count := range statusStats {
		values = append(values, "(?, ?)")
		args = append(args, group, count)
	}
	if len(values) > 0 {
		query := "INSERT INTO status_code_history (status_group, count) VALUES " +
			strings.Join(values, ",")
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("failed to save status stats: %v", err)
		}
	}

	// 保存路径统计
	if pathStats, ok := stats["top_paths"].([]PathMetrics); ok && len(pathStats) > 0 {
		pathStmt, err := tx.Prepare(`
			INSERT INTO popular_paths_history (
				path, request_count, error_count, avg_latency, bytes_transferred
			) VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare path statement: %v", err)
		}
		defer pathStmt.Close()

		for _, p := range pathStats {
			if _, err := pathStmt.Exec(
				p.Path, p.RequestCount, p.ErrorCount,
				p.AvgLatency, p.BytesTransferred,
			); err != nil {
				return fmt.Errorf("failed to save path stats: %v", err)
			}
		}
	}

	// 保存引用来源
	if refererStats, ok := stats["top_referers"].([]PathMetrics); ok && len(refererStats) > 0 {
		refererStmt, err := tx.Prepare(`
			INSERT INTO referer_history (referer, request_count)
			VALUES (?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare referer statement: %v", err)
		}
		defer refererStmt.Close()

		for _, r := range refererStats {
			if _, err := refererStmt.Exec(r.Path, r.RequestCount); err != nil {
				return fmt.Errorf("failed to save referer stats: %v", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	log.Printf("Saved all metrics in %v", time.Since(start))
	return nil
}
