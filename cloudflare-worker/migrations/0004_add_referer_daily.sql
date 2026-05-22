-- 引用来源 host 天级聚合表
-- 按 (host, ts_date, node_id) 三元组存储每天每 host 的请求 / 字节 / 错误数
-- 多节点写入互不覆盖; 查询时由 GROUP BY 在 worker 侧聚合
-- host 是从 Referer 头解析出来的小写域名 (不带端口 / 路径); 空 Referer 用空串
-- 仅记录当日 requests >= 10 的 host, 避免 cardinality 爆炸

CREATE TABLE IF NOT EXISTS referer_daily (
    host TEXT NOT NULL,
    ts_date INTEGER NOT NULL,        -- 自纪元的天数 (UTC, day = unix / 86400)
    node_id TEXT NOT NULL DEFAULT 'default',
    requests INTEGER DEFAULT 0,
    bytes INTEGER DEFAULT 0,
    errors INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (host, ts_date, node_id)
);

CREATE INDEX IF NOT EXISTS idx_referer_daily_ts_date ON referer_daily(ts_date DESC);
CREATE INDEX IF NOT EXISTS idx_referer_daily_host ON referer_daily(host);
CREATE INDEX IF NOT EXISTS idx_referer_daily_updated_at ON referer_daily(updated_at);
