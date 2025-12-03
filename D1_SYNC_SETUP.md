# D1 同步功能部署指南

本指南介绍如何设置 Cloudflare D1 数据库同步功能,用于在多个 proxy-go 节点之间同步配置和数据。

## 为什么使用 D1?

相比 S3 存储,D1 提供:
- ✅ **更低成本** - Cloudflare D1 免费额度更高
- ✅ **更快速度** - 数据库查询比对象存储更快
- ✅ **更简单** - 不需要管理 bucket 和访问密钥
- ✅ **更安全** - 使用 API token 而不是长期凭证

## 部署步骤

### 1. 部署 Cloudflare Worker

进入 Worker 项目目录:

```bash
cd cloudflare-worker
npm install
```

### 2. 创建 D1 数据库

```bash
npm run d1:create
```

这将输出一个 database ID,复制它并粘贴到 `wrangler.toml`:

```toml
[[d1_databases]]
binding = "DB"
database_name = "proxy-go-data"
database_id = "你的-database-id"  # 替换为实际 ID
```

### 3. 运行数据库迁移

```bash
npm run d1:migrations
```

这将创建 `config`, `path_stats`, `banned_ips` 三个表。

### 4. 设置 API Token (推荐)

为了安全,设置一个 API token:

```bash
# 使用 wrangler secret 命令(推荐,用于生产环境)
wrangler secret put API_TOKEN
# 输入你的 token,例如: your-secure-random-token-here

# 或者在 wrangler.toml 中设置(仅用于开发)
[vars]
API_TOKEN = "your-secure-token"
```

**安全建议**:
- 使用随机生成的强密码作为 token
- 生产环境必须使用 `wrangler secret` 而不是写在配置文件中
- 定期更换 token

### 5. 部署 Worker

```bash
npm run deploy
```

部署成功后,你会看到 Worker URL,例如:
```
https://proxy-go-sync.your-account.workers.dev
```

### 6. 配置 proxy-go 服务器

在你的 proxy-go 服务器上设置环境变量:

```bash
# 启用 D1 同步
export D1_SYNC_ENABLED=true

# Worker URL
export D1_SYNC_URL=https://proxy-go-sync.your-account.workers.dev

# API Token (必需,与 Worker 中设置的相同)
export D1_SYNC_TOKEN=your-secure-random-token-here
```

或者在 `.env` 文件中:

```env
D1_SYNC_ENABLED=true
D1_SYNC_URL=https://proxy-go-sync.your-account.workers.dev
D1_SYNC_TOKEN=your-secure-random-token-here
```

### 7. 重启 proxy-go 服务

```bash
# 重启服务
systemctl restart proxy-go

# 或者直接运行
./proxy-go
```

### 8. 验证同步

检查日志,确认看到:

```
[Sync] Initializing D1 sync service...
[Sync] D1 sync service initialized (endpoint: https://...)
[Sync] Sync service initialized successfully
```

## 数据迁移 (从 S3 迁移到 D1)

如果你之前使用 S3 同步,迁移步骤:

### 方法 1: 自动迁移

1. 保持 S3 环境变量不变
2. 添加 D1 环境变量
3. 设置 `D1_SYNC_ENABLED=true`
4. 重启服务 - 本地数据会自动上传到 D1

### 方法 2: 手动迁移

1. 从 S3 下载现有数据:
   - `config.json`
   - `path_stats.json`
   - `banned_ips.json`

2. 将文件放在 proxy-go 的 `data/` 目录

3. 配置 D1 环境变量并重启 - 数据会自动上传

### 方法 3: 使用 API 手动上传

```bash
# 上传配置
curl -X POST https://your-worker.workers.dev/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{"data": {...}}'
```

## 多节点部署

在多个服务器上部署 proxy-go 时:

1. **首个节点**: 按上述步骤完整配置,数据会自动上传到 D1
2. **其他节点**: 只需配置 D1 环境变量,启动时会从 D1 下载最新配置

所有节点共享同一个配置,任何节点的修改都会同步到其他节点。

## 验证同步状态

### 检查 Worker

访问 Worker URL:
```bash
curl https://your-worker.workers.dev/
```

应该返回 API 信息。

### 检查数据

```bash
# 获取配置
curl https://your-worker.workers.dev/config \
  -H "Authorization: Bearer your-token"

# 获取统计
curl https://your-worker.workers.dev/path_stats \
  -H "Authorization: Bearer your-token"

# 获取封禁IP
curl https://your-worker.workers.dev/banned_ips \
  -H "Authorization: Bearer your-token"
```

### 查看 D1 数据库

```bash
cd cloudflare-worker

# 查询数据
wrangler d1 execute proxy-go-data \
  --command "SELECT * FROM config"
```

## 故障排查

### 1. 无法连接到 Worker

**症状**: 日志显示 "failed to send request"

**解决**:
- 检查 `D1_SYNC_URL` 是否正确
- 确认 Worker 已成功部署
- 测试 Worker URL: `curl https://your-worker.workers.dev/`

### 2. 认证失败

**症状**: 日志显示 "Unauthorized" 或 "401"

**解决**:
- 检查 `D1_SYNC_TOKEN` 是否与 Worker 中设置的一致
- 确认 token 没有多余的空格或换行符
- 使用 `wrangler secret list` 查看 Worker 中的 secrets

### 3. 数据未同步

**症状**: 修改配置后其他节点没有更新

**解决**:
- 检查日志中的同步错误信息
- 确认所有节点使用相同的 Worker URL
- 手动触发同步: 重启服务或修改配置

### 4. D1 数据库错误

**症状**: Worker 返回 "D1 API error"

**解决**:
- 确认数据库迁移已执行: `npm run d1:migrations`
- 检查 `wrangler.toml` 中的 database_id 是否正确
- 查看 Worker 日志: `npm run tail`

## 成本估算

Cloudflare Workers 免费额度:
- **每天 100,000 次请求** (Workers)
- **每天 5,000,000 次读取** (D1)
- **每天 100,000 次写入** (D1)

对于一般使用场景:
- 单节点: ~1,000 次请求/天 (配置同步 + 统计同步)
- 10 节点: ~10,000 次请求/天

完全在免费额度内,无需付费。

## 高级配置

### 自定义同步间隔

D1Manager 默认每 10 分钟同步一次。如需修改:

在 `pkg/sync/d1_manager.go` 中修改:
```go
ticker := time.NewTicker(10 * time.Minute)  // 改为你需要的间隔
```

### 禁用自动同步

如果只想手动触发同步,可以修改 `D1Manager.Start()` 方法,注释掉 `go m.syncLoop(ctx)`。

### 监控同步状态

在管理后台 (即将实现) 可以查看:
- 最后同步时间
- 同步状态 (成功/失败)
- 远程版本 vs 本地版本

## 相关文档

- [cloudflare-worker/README.md](cloudflare-worker/README.md) - Worker 项目详细说明
- [CLAUDE.md](CLAUDE.md) - 完整的项目文档
- [Cloudflare D1 文档](https://developers.cloudflare.com/d1/)
- [Cloudflare Workers 文档](https://developers.cloudflare.com/workers/)
