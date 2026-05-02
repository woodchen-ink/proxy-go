# Proxy-Go Sync Worker

Cloudflare Worker，用 D1 数据库存放 proxy-go 的配置、路径统计、IP 封禁记录和指标。

## 🚀 一键部署（推荐，无需下载代码）

整个流程在浏览器里完成，**不需要本地装 Node.js / wrangler / git**。

### 1. Fork 仓库

打开 [woodchen-ink/proxy-go](https://github.com/woodchen-ink/proxy-go) → 右上角 **Fork**。

### 2. 准备 Cloudflare 凭据

| 字段 | 获取方式 |
|------|----------|
| `CLOUDFLARE_ACCOUNT_ID` | 登录 [Cloudflare Dashboard](https://dash.cloudflare.com) → 任意域名 Overview 页 → 右下侧栏 |
| `CLOUDFLARE_API_TOKEN` | 打开 https://dash.cloudflare.com/profile/api-tokens → **Create Token** → 选 "Edit Cloudflare Workers" 模板，并在 Account 资源里勾上 **D1:Edit** |

> Token 至少需要的权限：`Account → Workers Scripts:Edit`、`Account → D1:Edit`、`Account → Account Settings:Read`

### 3. 把凭据写入 fork 后的仓库

进入 fork 仓库 → **Settings → Secrets and variables → Actions → New repository secret**，添加两个：

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`

### 4. 触发一键部署

进入 **Actions → Deploy Cloudflare Worker → Run workflow**。

可选输入：
- Worker 名称（默认 `proxy-go-sync`）
- D1 数据库名称（默认 `proxy-go-data`）
- API Token（留空则随机生成 32 字节，安全推荐）

Workflow 会自动：
- 创建 D1 数据库（已存在则复用）
- 写好 `wrangler.toml`（本地不会留下任何文件）
- 远程应用所有 SQL migrations
- 把 API Token 注入 Worker secret
- 部署 Worker 并打印访问 URL

### 5. 把结果填回 proxy-go

部署完成后，**Workflow 的 Summary 页面**会显示：

```bash
D1_SYNC_ENABLED=true
D1_SYNC_URL=https://proxy-go-sync.<你的子域>.workers.dev
D1_SYNC_TOKEN=<自动生成的 token>
```

把这三行加到 proxy-go 的 `.env` 或 `docker-compose.yml`，重启 proxy-go 即可。

> ⚠️ Token 只在 Summary 页面显示一次，刷新后无法再次查看，请立即保存。如果丢失，重新跑 workflow 并提供自己的 API Token 即可覆盖。

---

## 🛠 本地部署（仅在你想改 Worker 源码时用）

```bash
cd cloudflare-worker
npm install

# 1. 创建 D1 数据库（输出 database_id）
npm run d1:create

# 2. 把 database_id 写入 wrangler.toml
cp sample.wrangler.toml wrangler.toml
# 编辑 wrangler.toml，填入 database_id

# 3. 应用迁移到远程 D1（注意 --remote）
npm run d1:remote

# 4. 设置 API Token
wrangler secret put API_TOKEN

# 5. 部署
npm run deploy
```

---

## API Endpoints

所有接口支持 CORS，返回 JSON。需要在 `Authorization: Bearer <token>` 头里带 token。

| 资源 | 方法 | 端点 |
|------|------|------|
| 路径统计 | GET / POST | `/path-stats` |
| 封禁 IP | GET / POST | `/banned-ips` |
| 封禁历史 | GET | `/banned-ips/history` |
| 路径配置 | GET / POST / DELETE | `/config-maps` |
| 系统配置 | GET / POST | `/config-other` |
| 状态码统计 | GET / POST | `/metrics/status-codes` |
| 延迟分布 | GET / POST | `/metrics/latency` |

详细参数和返回格式见 [src/index.ts](src/index.ts)。

---

## Database Schema

D1 数据库使用列式存储，5 张表：

| 表 | 用途 |
|------|------|
| `path_stats` | 每个路径的请求量/错误率/缓存命中率等 |
| `banned_ips` / `banned_ips_history` | 当前封禁与历史记录 |
| `config_maps` | 路径级配置（target、扩展规则、缓存策略） |
| `config_other` | 系统级配置（compression、security、cache、mirror_cache） |
| `status_codes` / `latency_distribution` | HTTP 状态码和延迟分桶 |

完整 schema 见 [migrations/](migrations/)。

---

## 与 Proxy-Go 集成

proxy-go 启动时：
- 从 D1 拉取最新配置（本地 `data/config.json` 仅作 fallback）
- 配置变更时即时同步到 D1（带 3s 防抖窗口合并连续修改）
- 每 30 分钟批量上传 metrics 和 path stats
- 进程退出时执行最后一次 flush

无需 S3、对象存储或本地共享卷。
