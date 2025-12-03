-- 重构数据库结构: JSON -> 列式存储
-- 迁移时间: 2025-12-03

-- ============================================
-- 1. 路径统计表 (path_stats)
-- ============================================
DROP TABLE IF EXISTS path_stats;

CREATE TABLE path_stats (
    path TEXT PRIMARY KEY,
    request_count INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    bytes_transferred INTEGER DEFAULT 0,
    status_2xx INTEGER DEFAULT 0,
    status_3xx INTEGER DEFAULT 0,
    status_4xx INTEGER DEFAULT 0,
    status_5xx INTEGER DEFAULT 0,
    cache_hits INTEGER DEFAULT 0,
    cache_misses INTEGER DEFAULT 0,
    cache_hit_rate REAL DEFAULT 0.0,
    bytes_saved INTEGER DEFAULT 0,
    avg_latency TEXT,
    last_access_time INTEGER,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_path_stats_request_count ON path_stats(request_count DESC);
CREATE INDEX idx_path_stats_updated_at ON path_stats(updated_at);
CREATE INDEX idx_path_stats_last_access ON path_stats(last_access_time);

-- ============================================
-- 2. IP封禁表 (banned_ips)
-- ============================================
DROP TABLE IF EXISTS banned_ips;

-- 当前封禁表
CREATE TABLE banned_ips (
    ip TEXT PRIMARY KEY,
    ban_time INTEGER NOT NULL,
    ban_end_time INTEGER NOT NULL,
    reason TEXT,
    error_count INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT 1,
    unban_time INTEGER,
    unban_reason TEXT,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_banned_ips_ban_end_time ON banned_ips(ban_end_time);
CREATE INDEX idx_banned_ips_is_active ON banned_ips(is_active);
CREATE INDEX idx_banned_ips_updated_at ON banned_ips(updated_at);

-- 封禁历史表
CREATE TABLE banned_ips_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ip TEXT NOT NULL,
    ban_time INTEGER NOT NULL,
    ban_end_time INTEGER NOT NULL,
    reason TEXT,
    error_count INTEGER DEFAULT 0,
    unban_time INTEGER,
    unban_reason TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_banned_ips_history_ip ON banned_ips_history(ip);
CREATE INDEX idx_banned_ips_history_created_at ON banned_ips_history(created_at DESC);

-- ============================================
-- 3. 路径配置表 (config_maps)
-- ============================================
DROP TABLE IF EXISTS config_maps;

CREATE TABLE config_maps (
    path TEXT PRIMARY KEY,
    default_target TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    extension_rules TEXT,        -- JSON: 扩展名规则
    cache_config TEXT,           -- JSON: 缓存配置
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_config_maps_enabled ON config_maps(enabled);
CREATE INDEX idx_config_maps_updated_at ON config_maps(updated_at);

-- ============================================
-- 4. 系统配置表 (config_other)
-- ============================================
DROP TABLE IF EXISTS config_other;

CREATE TABLE config_other (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,         -- JSON: 配置值
    description TEXT,            -- 配置说明
    updated_at INTEGER NOT NULL
);

-- 预定义配置键
-- key: "compression" - 压缩配置
-- key: "security" - 安全配置
-- key: "cache" - 缓存配置
-- key: "mirror_cache" - Mirror缓存配置

CREATE INDEX idx_config_other_updated_at ON config_other(updated_at);

-- ============================================
-- 5. 删除旧的 config 表 (可选)
-- ============================================
-- 保留 config 表作为备份/兼容,或者删除:
-- DROP TABLE IF EXISTS config;
