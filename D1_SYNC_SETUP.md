# D1 同步部署指南

proxy-go 用 Cloudflare D1 在多节点间同步配置和指标。下面是两条部署路径：**一键部署（推荐）** 和 **本地部署（改源码时用）**。

---

## 一键部署（无需下载任何代码）

整个流程在浏览器里完成。

### 1. Fork 本仓库

打开仓库主页 → 右上角 **Fork**。

### 2. 在 Cloudflare 拿两份凭据

| 字段 | 在哪拿 |
|------|--------|
| `CLOUDFLARE_ACCOUNT_ID` | [Dashboard](https://dash.cloudflare.com) → 任意域名 Overview → 右下侧栏 |
| `CLOUDFLARE_API_TOKEN` | https://dash.cloudflare.com/profile/api-tokens → **Create Token** → 选 *Edit Cloudflare Workers* 模板，并在 Account 资源里勾上 **D1:Edit** |

最低需要的权限：
- Account → **Workers Scripts:Edit**
- Account → **D1:Edit**
- Account → **Account Settings:Read**

### 3. 把凭据加到 fork 的仓库

进入 fork 后的仓库 → **Settings → Secrets and variables → Actions**，添加两个 Repository secret：
- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`

### 4. 触发部署

Actions → **Deploy Cloudflare Worker** → **Run workflow**。

可选输入：
- Worker 名称（默认 `proxy-go-sync`）
- D1 数据库名称（默认 `proxy-go-data`）
- API Token（留空 → 自动生成 32 字节随机值）

Workflow 会自动完成：
- 创建（或复用）D1 数据库
- 写入 `wrangler.toml`
- 远程应用所有 SQL migrations
- 把 API Token 注入 Worker secret
- 部署 Worker 并打印访问 URL

### 5. 把结果写回 proxy-go

Workflow 跑完后进入 **Summary** 页面，会显示三行：

```bash
D1_SYNC_ENABLED=true
D1_SYNC_URL=https://proxy-go-sync.<你的子域>.workers.dev
D1_SYNC_TOKEN=<自动生成的 token>
```

加到 proxy-go 的 `.env` 或 `docker-compose.yml`，重启 proxy-go 即可。

> ⚠️ Token 只在 Summary 显示一次，刷新后无法再查看。丢失后重新跑 workflow 并自己提供新 Token 即可。

---

## 本地部署（仅改 Worker 源码时用）

```bash
git clone <repo>
cd proxy-go/cloudflare-worker
npm install

# 1. 创建 D1 数据库
npm run d1:create
# 输出末尾会有 database_id，复制它

# 2. 配置 wrangler.toml
cp sample.wrangler.toml wrangler.toml
# 编辑 wrangler.toml，把 database_id 填进去

# 3. 应用迁移（注意 --remote）
npm run d1:remote

# 4. 设置 API Token
wrangler secret put API_TOKEN
# 提示输入 token 时粘贴

# 5. 部署
npm run deploy
```

注意事项：
- D1 有"本地"和"远程"两个独立环境。Worker 上线后只访问 **远程**，所以一定要带 `--remote`，否则 Worker 看到的是空表。
- `wrangler.toml` 含敏感信息（database_id 不算敏感，但 `[vars] API_TOKEN` 算）。仓库 `.gitignore` 已忽略它。

---

## proxy-go 端配置

无论哪种部署方式，proxy-go 端都只需要这三个环境变量：

```bash
D1_SYNC_ENABLED=true
D1_SYNC_URL=https://proxy-go-sync.<你的子域>.workers.dev
D1_SYNC_TOKEN=<和 Worker 的 API_TOKEN 完全一致>
```

启动后日志中应有：

```
[Sync] D1 sync service initialized
[Sync] Downloading config from D1...
[Sync] Config downloaded successfully
```

---

## 同步策略

| 数据 | 触发方式 | 频率 |
|------|---------|------|
| Config | 配置变更时 | 即时（3s 防抖合并连续修改） |
| Path Stats | 后台任务 | 每 30 分钟 |
| Status Codes / Latency | 后台任务 | 每 30 分钟 |
| Banned IPs | IP 封禁/解封时 | 即时 |

进程退出（SIGTERM）时会执行最后一次 flush，保证数据不丢。

---

## 常见问题

**Q: workflow 报错 "Could not find database"**
A: D1 创建可能因 Token 权限不足失败，确认 Token 勾选了 `Account → D1:Edit`。

**Q: Worker URL 是 `*.workers.dev`，能换成自定义域名吗？**
A: 可以。在 Cloudflare Dashboard → Workers 找到该 Worker → **Triggers → Custom Domains** → 添加。proxy-go 端把 `D1_SYNC_URL` 换成自定义域名即可。

**Q: 部署后忘了 token，怎么办？**
A: 重新跑 workflow，输入框里填一个新的（或留空让它生成）。Worker secret 会被覆盖，Summary 里能再看一次。同时 proxy-go 端要同步更新 `D1_SYNC_TOKEN`。

**Q: 想完全删除部署？**
A: Cloudflare Dashboard → Workers & Pages 里删除 Worker；Workers → D1 里删除数据库。
