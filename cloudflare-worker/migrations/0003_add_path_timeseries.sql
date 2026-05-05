-- 路径时间序列表
-- 按 (path, ts_hour, node_id) 三元组存储每小时的请求 / 字节 / 错误数
-- 多节点写入互不覆盖; 查询时由 GROUP BY 在 worker 侧聚合

DROP TABLE IF EXISTS path_timeseries;

CREATE TABLE path_timeseries (
    path TEXT NOT NULL,
    ts_hour INTEGER NOT NULL,        -- 自纪元的小时数 (UTC)
    node_id TEXT NOT NULL DEFAULT 'default',
    requests INTEGER DEFAULT 0,
    bytes INTEGER DEFAULT 0,
    errors INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (path, ts_hour, node_id)
);

CREATE INDEX idx_path_timeseries_ts_hour ON path_timeseries(ts_hour DESC);
CREATE INDEX idx_path_timeseries_path ON path_timeseries(path);
CREATE INDEX idx_path_timeseries_updated_at ON path_timeseries(updated_at);
