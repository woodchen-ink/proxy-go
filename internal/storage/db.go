package storage

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func InitDB(db *sql.DB) error {
	// 优化SQLite配置
	_, err := db.Exec(`
        PRAGMA journal_mode = WAL;
        PRAGMA synchronous = NORMAL;
        PRAGMA cache_size = 1000000;
        PRAGMA temp_store = MEMORY;
    `)
	if err != nil {
		return err
	}

	// 创建表
	if err := initTables(db); err != nil {
		return err
	}

	// 启动定期清理
	go cleanupRoutine(db)

	return nil
}

func initTables(db *sql.DB) error {
	// 基础指标表
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS metrics_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            total_requests INTEGER,
            total_errors INTEGER,
            total_bytes INTEGER,
            avg_latency INTEGER
        )
    `)
	if err != nil {
		return err
	}

	// 状态码统计表
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

	// 路径统计表
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

	// 添加索引
	_, err = db.Exec(`
        CREATE INDEX IF NOT EXISTS idx_timestamp ON metrics_history(timestamp);
        CREATE INDEX IF NOT EXISTS idx_path ON path_stats(path);
    `)
	return err
}

func cleanupRoutine(db *sql.DB) {
	// 批量删除而不是单条删除
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		return
	}
	defer tx.Rollback()

	// 保留90天的数据
	_, err = tx.Exec(`
        DELETE FROM metrics_history 
        WHERE timestamp < datetime('now', '-90 days')
    `)
	if err != nil {
		log.Printf("Error cleaning old data: %v", err)
	}

	// 清理状态码统计
	_, err = tx.Exec(`
        DELETE FROM status_stats 
        WHERE timestamp < datetime('now', '-90 days')
    `)
	if err != nil {
		log.Printf("Error cleaning old data: %v", err)
	}

	// 清理路径统计
	_, err = tx.Exec(`
        DELETE FROM path_stats 
        WHERE timestamp < datetime('now', '-90 days')
    `)
	if err != nil {
		log.Printf("Error cleaning old data: %v", err)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
	}
}
