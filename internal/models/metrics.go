package models

import (
	"database/sql"
	"log"
	"proxy-go/internal/constants"
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
	DB *sql.DB
}

func NewMetricsDB(dbPath string) (*MetricsDB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 创建必要的表
	if err := initTables(db); err != nil {
		db.Close()
		return nil, err
	}

	return &MetricsDB{DB: db}, nil
}

func initTables(db *sql.DB) error {
	// 创建指标历史表
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

		-- 复合索引，用于优化聚合查询
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
	_, err = db.Exec(`
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
	`)

	// 启动定期清理任务
	go cleanupRoutine(db)

	return nil
}

// 定期清理旧数据
func cleanupRoutine(db *sql.DB) {
	ticker := time.NewTicker(constants.CleanupInterval)
	for range ticker.C {
		// 开始事务
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Error starting cleanup transaction: %v", err)
			continue
		}

		// 删除超过保留期限的数据
		cutoff := time.Now().Add(-constants.DataRetention)
		_, err = tx.Exec(`DELETE FROM metrics_history WHERE timestamp < ?`, cutoff)
		if err != nil {
			tx.Rollback()
			log.Printf("Error cleaning metrics_history: %v", err)
			continue
		}

		_, err = tx.Exec(`DELETE FROM status_stats WHERE timestamp < ?`, cutoff)
		if err != nil {
			tx.Rollback()
			log.Printf("Error cleaning status_stats: %v", err)
			continue
		}

		_, err = tx.Exec(`DELETE FROM path_stats WHERE timestamp < ?`, cutoff)
		if err != nil {
			tx.Rollback()
			log.Printf("Error cleaning path_stats: %v", err)
			continue
		}

		// 提交事务
		if err := tx.Commit(); err != nil {
			log.Printf("Error committing cleanup transaction: %v", err)
		} else {
			log.Printf("Successfully cleaned up old metrics data")
		}
	}
}

func (db *MetricsDB) SaveMetrics(stats map[string]interface{}) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 保存基础指标
	_, err = tx.Exec(`
		INSERT INTO metrics_history (
			total_requests, total_errors, total_bytes, avg_latency
		) VALUES (?, ?, ?, ?)`,
		stats["total_requests"], stats["total_errors"],
		stats["total_bytes"], stats["avg_latency"],
	)
	if err != nil {
		return err
	}

	// 保存状态码统计
	statusStats := stats["status_code_stats"].(map[string]int64)
	stmt, err := tx.Prepare(`
		INSERT INTO status_stats (status_group, count)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for group, count := range statusStats {
		if _, err := stmt.Exec(group, count); err != nil {
			return err
		}
	}

	// 保存路径统计
	pathStats := stats["top_paths"].([]PathMetrics)
	stmt, err = tx.Prepare(`
		INSERT INTO path_stats (
			path, requests, errors, bytes, avg_latency
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	for _, p := range pathStats {
		if _, err := stmt.Exec(
			p.Path, p.RequestCount, p.ErrorCount,
			p.BytesTransferred, p.AvgLatency,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *MetricsDB) Close() error {
	return db.DB.Close()
}

func (db *MetricsDB) GetRecentMetrics(hours int) ([]HistoricalMetrics, error) {
	var interval string
	if hours <= 24 {
		interval = "%Y-%m-%d %H:%M:00" // 按分钟分组
	} else if hours <= 168 {
		interval = "%Y-%m-%d %H:00:00" // 按小时分组
	} else {
		interval = "%Y-%m-%d 00:00:00" // 按天分组
	}

	// 修改查询逻辑，使用窗口函数计算差值
	rows, err := db.DB.Query(`
		WITH time_series AS (
			-- 生成时间序列
			SELECT datetime('now', 'localtime') - 
				   (CASE 
						WHEN ?2 <= 24 THEN (300 * n) -- 5分钟间隔
						WHEN ?2 <= 168 THEN (3600 * n) -- 1小时间隔
						ELSE (86400 * n) -- 1天间隔
					END) AS time_point
			FROM (
				SELECT ROW_NUMBER() OVER () - 1 as n
				FROM metrics_history
				LIMIT (CASE 
					WHEN ?2 <= 24 THEN ?2 * 12 -- 5分钟间隔
					WHEN ?2 <= 168 THEN ?2 -- 1小时间隔
					ELSE ?2 / 24 -- 1天间隔
				END)
			)
			WHERE time_point >= datetime('now', '-' || ?2 || ' hours', 'localtime')
			  AND time_point <= datetime('now', 'localtime')
		),
		grouped_metrics AS (
			SELECT 
				strftime(?1, timestamp, 'localtime') as group_time,
				MAX(total_requests) as period_requests,
				MAX(total_errors) as period_errors,
				MAX(total_bytes) as period_bytes,
				AVG(avg_latency) as avg_latency
			FROM metrics_history
			WHERE timestamp >= datetime('now', '-' || ?2 || ' hours', 'localtime')
			GROUP BY group_time
		)
		SELECT 
			strftime(?1, ts.time_point, 'localtime') as timestamp,
			COALESCE(m.period_requests - LAG(m.period_requests, 1) OVER (ORDER BY ts.time_point), 0) as total_requests,
			COALESCE(m.period_errors - LAG(m.period_errors, 1) OVER (ORDER BY ts.time_point), 0) as total_errors,
			COALESCE(m.period_bytes - LAG(m.period_bytes, 1) OVER (ORDER BY ts.time_point), 0) as total_bytes,
			COALESCE(m.avg_latency, 0) as avg_latency
		FROM time_series ts
		LEFT JOIN grouped_metrics m ON strftime(?1, ts.time_point, 'localtime') = m.group_time
		ORDER BY timestamp DESC
	`, interval, hours)
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
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 保存性能指标
	_, err = tx.Exec(`
		INSERT INTO performance_metrics (
			avg_response_time,
			requests_per_second,
			bytes_per_second
		) VALUES (?, ?, ?)`,
		stats["avg_latency"],
		stats["requests_per_second"],
		stats["bytes_per_second"],
	)
	if err != nil {
		return err
	}

	// 保存状态码统计
	statusStats := stats["status_code_stats"].(map[string]int64)
	stmt, err := tx.Prepare(`
		INSERT INTO status_code_history (status_group, count)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for group, count := range statusStats {
		if _, err := stmt.Exec(group, count); err != nil {
			return err
		}
	}

	// 保存热门路径
	pathStats := stats["top_paths"].([]PathMetrics)
	stmt, err = tx.Prepare(`
		INSERT INTO popular_paths_history (
			path, request_count, error_count, avg_latency, bytes_transferred
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	for _, p := range pathStats {
		if _, err := stmt.Exec(
			p.Path, p.RequestCount, p.ErrorCount,
			p.AvgLatency, p.BytesTransferred,
		); err != nil {
			return err
		}
	}

	// 保存引用来源
	refererStats := stats["top_referers"].([]PathMetrics)
	stmt, err = tx.Prepare(`
		INSERT INTO referer_history (referer, request_count)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}

	for _, r := range refererStats {
		if _, err := stmt.Exec(r.Path, r.RequestCount); err != nil {
			return err
		}
	}

	return tx.Commit()
}
