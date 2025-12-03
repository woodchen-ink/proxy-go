-- 配置表 (存储 config.json)
CREATE TABLE IF NOT EXISTS config (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (id = 1)  -- 确保只有一行
);

-- 路径统计表 (存储 path_stats.json)
CREATE TABLE IF NOT EXISTS path_stats (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (id = 1)  -- 确保只有一行
);

-- IP封禁表 (存储 banned_ips.json)
CREATE TABLE IF NOT EXISTS banned_ips (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (id = 1)  -- 确保只有一行
);

-- 创建索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_config_updated_at ON config(updated_at);
CREATE INDEX IF NOT EXISTS idx_path_stats_updated_at ON path_stats(updated_at);
CREATE INDEX IF NOT EXISTS idx_banned_ips_updated_at ON banned_ips(updated_at);
