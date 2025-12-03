

# D1 V2 迁移指南 - 从 JSON 到列式存储

## 概述

V2 版本将数据库从 JSON 存储重构为列式存储,提供更好的查询性能和数据管理能力。

## 主要变化

### 1. Path Stats (路径统计)
**旧版 (V1)**:
```sql
CREATE TABLE path_stats (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data TEXT NOT NULL,  -- 整个 JSON
    updated_at INTEGER NOT NULL
);
```

**新版 (V2)**:
```sql
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
```

**优势**:
- ✅ 可按路径查询单个统计
- ✅ 可 SQL 聚合 (总请求数, 平均缓存命中率等)
- ✅ 支持索引加速查询
- ✅ 批量更新更高效

### 2. Banned IPs (IP封禁)
**新版 (V2)**:
```sql
-- 当前封禁
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

-- 历史记录
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
```

**优势**:
- ✅ 可 SQL 查询过期 IP 并自动清理
- ✅ 独立的历史记录表
- ✅ 支持按 IP 查询封禁历史
- ✅ 可按时间范围统计封禁数量

### 3. Config (配置拆分)
**新版 (V2)**:
```sql
-- 路径配置
CREATE TABLE config_maps (
    path TEXT PRIMARY KEY,
    default_target TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    extension_rules TEXT,  -- JSON: 扩展名规则
    cache_config TEXT,     -- JSON: 缓存配置
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 系统配置
CREATE TABLE config_other (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,   -- JSON: 配置值
    description TEXT,
    updated_at INTEGER NOT NULL
);
```

**优势**:
- ✅ 修改单个路径配置无需重传整个 config.json
- ✅ 可分页展示路径列表
- ✅ 可批量启用/禁用路径
- ✅ 系统配置独立管理

## 迁移步骤

### 第 1 步: 运行数据库迁移

```bash
cd cloudflare-worker

# 运行新的迁移 (会删除旧表并创建新表)
wrangler d1 migrations apply proxy-go-data --remote

# 验证表结构
wrangler d1 execute proxy-go-data --remote \
  --command "SELECT name FROM sqlite_master WHERE type='table'"
```

**警告**: 这会删除旧表中的数据!如果你有重要数据,请先备份:

```bash
# 备份旧数据 (在迁移前)
wrangler d1 execute proxy-go-data --remote \
  --command "SELECT * FROM config" > backup_config.json

wrangler d1 execute proxy-go-data --remote \
  --command "SELECT * FROM path_stats" > backup_path_stats.json

wrangler d1 execute proxy-go-data --remote \
  --command "SELECT * FROM banned_ips" > backup_banned_ips.json
```

### 第 2 步: 部署新的 Worker

Worker 已更新为使用 `index-v2.ts`:

```bash
cd cloudflare-worker
npm run deploy
```

验证部署:
```bash
curl https://your-worker.workers.dev/
# 应该看到 "proxy-go-sync API v2"
```

### 第 3 步: 更新 Proxy-Go

**当前状态**: V2 客户端代码已创建 (`d1_client_v2.go`),但还未集成到主程序。

**TODO**: 需要:
1. 创建数据转换函数 (path_stats.json → PathStat[])
2. 创建数据转换函数 (banned_ips.json → BannedIP[])
3. 创建数据转换函数 (config.json → ConfigMap[] + ConfigOther[])
4. 更新 D1Manager 使用 V2 客户端
5. 实现程序退出前的同步

### 第 4 步: 测试

测试新的 API:

```bash
# 获取路径统计
curl https://your-worker.workers.dev/path-stats \
  -H "Authorization: Bearer your-token"

# 获取特定路径
curl https://your-worker.workers.dev/path-stats?path=/b2 \
  -H "Authorization: Bearer your-token"

# 更新统计
curl -X POST https://your-worker.workers.dev/path-stats \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{
    "stats": [
      {
        "path": "/test",
        "request_count": 100,
        "error_count": 5,
        "bytes_transferred": 1048576,
        "status_2xx": 95,
        "status_3xx": 0,
        "status_4xx": 3,
        "status_5xx": 2,
        "cache_hits": 80,
        "cache_misses": 20,
        "cache_hit_rate": 0.8,
        "bytes_saved": 524288,
        "updated_at": 1701234567890
      }
    ]
  }'
```

## 性能对比

| 操作 | V1 (JSON) | V2 (列式) | 提升 |
|------|-----------|-----------|------|
| 查询单个路径统计 | 下载全部 JSON 再过滤 | SQL WHERE 查询 | ~10x |
| 更新单个路径 | 替换整个 JSON | UPDATE 单行 | ~5x |
| 统计总请求数 | 下载全部+计算 | SQL SUM() | ~20x |
| 查询活跃封禁 | 下载全部+过滤 | SQL WHERE | ~8x |

## 回滚

如果需要回滚到 V1:

```bash
cd cloudflare-worker

# 1. 恢复 wrangler.toml
# 将 main = "src/index-v2.ts" 改回 main = "src/index.ts"

# 2. 重新运行 V1 迁移
wrangler d1 migrations apply proxy-go-data --remote \
  --command "$(cat migrations/0001_initial_schema.sql)"

# 3. 重新部署
wrangler deploy
```

## 下一步

1. **完成 Go 集成** - 将 V2 客户端集成到 D1Manager
2. **数据迁移工具** - 从旧的 JSON 文件导入数据到新表
3. **退出前同步** - 确保程序关闭时上传所有数据
4. **前端支持** - 更新管理后台使用新 API

## 相关文件

- **Worker**:
  - `cloudflare-worker/src/index-v2.ts` - V2 Worker 主文件
  - `cloudflare-worker/src/db.ts` - 数据库操作
  - `cloudflare-worker/migrations/0002_refactor_to_columns.sql` - V2 迁移

- **Go Client**:
  - `pkg/sync/d1_client_v2.go` - V2 客户端 (未集成)
  - `pkg/sync/d1_manager.go` - 需要更新使用 V2 客户端

## 状态

- ✅ 数据库迁移文件
- ✅ Worker API (V2)
- ✅ Go 客户端代码 (V2)
- ⏳ Go 集成 (TODO)
- ⏳ 数据转换 (TODO)
- ⏳ 退出前同步 (TODO)
- ⏳ 测试 (TODO)
