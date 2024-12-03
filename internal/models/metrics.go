package models

import (
	"database/sql"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	return &MetricsDB{DB: db}, nil
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
