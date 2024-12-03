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
	AvgLatency    int64   `json:"avg_latency"`
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
	`)
	if err != nil {
		return err
	}

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
		interval = "%Y-%m-%d %H:%M:00"
	} else if hours <= 168 {
		interval = "%Y-%m-%d %H:00:00"
	} else {
		interval = "%Y-%m-%d 00:00:00"
	}

	rows, err := db.DB.Query(`
		WITH grouped_metrics AS (
			SELECT 
				strftime(?1, timestamp) as group_time,
				SUM(total_requests) as total_requests,
				SUM(total_errors) as total_errors,
				SUM(total_bytes) as total_bytes,
				AVG(avg_latency) as avg_latency
			FROM metrics_history
			WHERE timestamp >= datetime('now', '-' || ?2 || ' hours')
			GROUP BY group_time
			ORDER BY group_time DESC
		)
		SELECT * FROM grouped_metrics
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
		if m.TotalRequests > 0 {
			m.ErrorRate = float64(m.TotalErrors) / float64(m.TotalRequests)
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}
