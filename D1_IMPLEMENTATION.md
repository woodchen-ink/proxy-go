# D1 列式存储实现总结

## 概述

完成了从 JSON 存储到 D1 列式存储的重构，所有数据现在以结构化列的形式存储在 Cloudflare D1 数据库中，提升了查询性能和数据管理能力。

## 架构变更

### 数据存储结构

**之前 (JSON 存储)**:
```json
{
  "path_stats": {...},
  "banned_ips": {...},
  "config": {...}
}
```

**现在 (列式存储)**:
- `path_stats` → 独立列（path, request_count, error_count, bytes_transferred, ...）
- `banned_ips` → 独立列（ip, ban_time, ban_end_time, reason, error_count, ...）
- `banned_ips_history` → 历史记录表
- `config_maps` → 路径配置表（一个路径一行）
- `config_other` → 键值对配置表（系统配置）

## 核心文件

### Cloudflare Worker

1. **cloudflare-worker/migrations/0001_initial_schema.sql**
   - 定义了 5 个表的结构
   - path_stats: 路径统计（15 列）
   - banned_ips: IP 封禁（9 列）
   - banned_ips_history: 封禁历史（8 列 + 自增 ID）
   - config_maps: 路径配置（7 列）
   - config_other: 系统配置（4 列）

2. **cloudflare-worker/src/index.ts**
   - RESTful API 端点
   - 批量 UPSERT 支持
   - Bearer Token 认证

3. **cloudflare-worker/src/db.ts**
   - TypeScript 类型定义
   - 数据库操作辅助函数
   - 批量操作使用 D1 batch API

### Go 后端

1. **pkg/sync/d1_client.go**
   - D1 HTTP 客户端
   - 类型定义与 Worker 完全对应
   - 批量上传/查询方法

2. **pkg/sync/d1_converter.go**
   - 数据格式转换函数
   - `ConvertPathStatsFromFile()` - 转换路径统计
   - `ConvertBannedIPsFromFile()` - 转换封禁 IP
   - `ConvertBannedIPHistoryFromFile()` - 转换封禁历史
   - `ConvertConfigFromFile()` - 转换配置（拆分为 MAP 和 Other）

3. **pkg/sync/d1_manager.go**
   - D1 同步管理器
   - `SyncNow()` - 完整同步（config + path_stats + banned_ips）
   - `UploadConfig()` - 配置上传（使用转换器）
   - `downloadConfigWithFallback()` - 配置下载（重建 JSON 格式）
   - 自动转换数据格式

4. **pkg/sync/service.go**
   - `StopSyncService()` - **退出前自动同步**
   - 在程序关闭时触发一次完整同步
   - 30 秒超时保护

## 数据转换逻辑

### Config 转换

**原始格式** (config.json):
```json
{
  "MAP": {
    "/path1": {
      "DefaultTarget": "https://example.com",
      "Enabled": true,
      "ExtensionMap": {...},
      "CacheConfig": {...}
    }
  },
  "Cache": {...},
  "Compression": {...}
}
```

**转换后**:
- ConfigMaps 表:
  ```
  path | default_target | enabled | extension_rules (JSON) | cache_config (JSON)
  ```
- ConfigOther 表:
  ```
  key (Cache/Compression/...) | value (JSON)
  ```

### 下载时重建

下载配置时，D1Manager 会：
1. 查询 ConfigMaps 和 ConfigOther
2. 重建原始 JSON 结构
3. 保存到本地 config.json

这样确保了与现有代码的兼容性。

## 同步流程

### 启动时

1. **InitSyncService()** - 初始化同步服务
2. **DownloadConfigOnly()** - 下载远程配置
   - 如果远程没有配置，上传本地配置作为初始版本
   - 如果远程有配置，下载并重建本地配置

### 运行时

1. **定时同步** - 每 10 分钟自动同步
   - 同步 config.json（转换为 ConfigMaps + ConfigOther）
   - 同步 path_stats.json（转换为 PathStat[]）
   - 同步 banned_ips.json（转换为 BannedIP[]）

2. **配置变更同步** - 通过回调触发
   - config 保存时自动上传到 D1

### 退出时

**StopSyncService()** - 执行最后一次完整同步
```go
log.Printf("[Sync] Performing final sync before shutdown...")
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := globalSyncService.manager.SyncNow(ctx); err != nil {
    log.Printf("[Sync] Warning: Final sync failed: %v", err)
} else {
    log.Printf("[Sync] Final sync completed successfully")
}
```

## 环境变量配置

```bash
# D1 Worker 配置
D1_ENDPOINT=https://your-worker.your-subdomain.workers.dev
D1_TOKEN=your-secure-token

# 旧的 S3 配置仍然支持（向后兼容）
S3_ENDPOINT=...
S3_BUCKET=...
```

优先级: D1 > S3

## 部署步骤

### 1. 部署 Cloudflare Worker

```bash
cd cloudflare-worker

# 创建 D1 数据库（如果还没有）
wrangler d1 create proxy-go-data

# 更新 wrangler.toml 中的 database_id

# 运行迁移（重要：必须使用 --remote）
wrangler d1 migrations apply proxy-go-data --remote

# 部署 Worker
wrangler deploy

# 设置 API Token（生产环境）
wrangler secret put API_TOKEN
```

### 2. 配置 Go 后端

```bash
# 设置环境变量
export D1_ENDPOINT=https://your-worker.your-subdomain.workers.dev
export D1_TOKEN=your-secure-token

# 启动服务
./proxy-go
```

## 数据迁移

如果已有 JSON 数据文件，不需要手动迁移：

1. 启动服务时会自动检测 D1 是否有数据
2. 如果 D1 为空，会自动上传本地 JSON 文件数据
3. 转换器会自动处理格式转换

手动迁移（如果需要）:
```bash
# 触发完整同步
curl -X POST http://localhost:3336/admin/api/sync/now
```

## API 端点

### Worker API

```
GET  /path-stats                  # 查询路径统计
POST /path-stats                  # 批量上传路径统计
GET  /banned-ips                  # 查询封禁 IP
POST /banned-ips                  # 批量上传封禁 IP
GET  /banned-ips/history          # 查询封禁历史
GET  /config-maps                 # 查询路径配置
POST /config-maps                 # 批量上传路径配置
GET  /config-other                # 查询系统配置
POST /config-other                # 批量上传系统配置
```

### Proxy-Go Admin API

```
POST /admin/api/sync/now          # 立即触发完整同步
GET  /admin/api/sync/status       # 查询同步状态
```

## 性能优化

1. **批量操作** - 使用 D1 batch API，一次性处理多行
2. **UPSERT** - INSERT ... ON CONFLICT，避免先查询再插入
3. **索引** - 主键自动索引（path, ip, key）
4. **按需同步** - 封禁历史暂不同步，避免重复写入

## 注意事项

1. **时间戳格式** - 统一使用 UnixMilli（毫秒时间戳）
2. **JSON 字段** - ExtensionRules 和 CacheConfig 存为 JSON TEXT
3. **布尔值** - SQLite 使用 INTEGER (0/1)，自动转换
4. **退出同步** - 程序退出时自动触发一次完整同步，确保数据不丢失

## 向后兼容

- S3 同步仍然支持（如果同时配置了 D1 和 S3，优先使用 D1）
- 本地 JSON 文件格式不变（配置、统计、封禁列表）
- 现有 API 端点不变
- 前端界面无需修改

## 测试清单

- [ ] Worker 部署成功
- [ ] D1 迁移执行成功（--remote）
- [ ] 启动时自动下载配置
- [ ] 定时同步正常工作（10 分钟）
- [ ] 配置变更自动同步
- [ ] 退出时完整同步
- [ ] 数据格式转换正确
- [ ] 批量操作性能良好
