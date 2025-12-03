-- 添加 Metrics 统计表
-- 迁移时间: 2025-12-03

-- ============================================
-- 1. 状态码统计表 (status_codes)
-- ============================================
DROP TABLE IF EXISTS status_codes;

CREATE TABLE status_codes (
    status_code TEXT PRIMARY KEY,
    count INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_status_codes_updated_at ON status_codes(updated_at);
CREATE INDEX idx_status_codes_count ON status_codes(count DESC);

-- ============================================
-- 2. 延迟分布表 (latency_distribution)
-- ============================================
DROP TABLE IF EXISTS latency_distribution;

CREATE TABLE latency_distribution (
    bucket TEXT PRIMARY KEY,  -- 延迟桶: "lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"
    count INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_latency_distribution_updated_at ON latency_distribution(updated_at);
CREATE INDEX idx_latency_distribution_count ON latency_distribution(count DESC);

-- 预插入延迟桶（可选）
INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES
    ('lt10ms', 0, 0),
    ('10-50ms', 0, 0),
    ('50-200ms', 0, 0),
    ('200-1000ms', 0, 0),
    ('gt1s', 0, 0);
