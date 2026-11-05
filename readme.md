# Proxy-Go

高性能反向代理 (Go), 配一个跑在 Cloudflare 上的中控 (Worker + Durable Object + D1)。节点只负责代理, 管理后台、配置下发、全集群清缓存、统计汇总、DNS 故障切换都在中控完成, 全部在 Cloudflare 免费额度内运行。

使用说明 / 部署帖: https://www.sunai.net/t/topic/165

镜像: `ghcr.io/woodchen-ink/proxy-go:latest`

## 架构

```
管理员浏览器 ──> 中控 (Cloudflare Worker, 你的管理域名)
                 ├─ 管理后台 / 管理 API (CZL Connect 登录)
                 ├─ 公开清缓存接口 /api/cache/clear-url
                 └─ Hub (Durable Object): 节点 WebSocket / 指令下发 / 实时快照 / 健康巡检 / DNS 切换
                         ▲ WebSocket (节点主动连出)          D1: 配置 + 累计统计
节点 A / 节点 B / 节点 C (Docker, 只跑代理, 不带管理界面)
```

- **改配置**: 后台保存后几秒内全部节点生效 (中控广播, 逐节点回执)
- **清缓存**: 一次清全部节点, 再清 Cloudflare / EdgeOne 的 CDN 缓存
- **仪表盘**: 在线节点 15 秒实时快照, 离线节点显示最后上报值
- **DNS 分流与故障切换**: 节点挂了自动改 CF for SaaS 回源 + DNSPod 线路记录, 或 EdgeOne 源站; 首选节点恢复后自动切回; 全程推送企业微信

## 功能特性

- 🚀 **多路径代理**: 不同路径代理到不同源站, 支持多源按序故障转移
- 🔄 **扩展名规则**: 按文件扩展名、大小、请求域名选择目标服务器
- 🎯 **302 跳转**: 路径级 / 扩展名级 302 模式
- 📦 **智能缓存**: 默认按 URL 缓存, 可按路径开启 Cloudflare Images 兼容的按格式分桶
- 🕒 **客户端缓存头**: 按路径缓存时长下发 `Cache-Control`
- 🛡️ **Referer 黑白名单 / 重定向**, IP 封禁
- 🎛️ **中控**: 全集群配置 / 清缓存 / 统计 / 节点状态 / DNS 故障切换 / 企业微信告警

## 部署

需要: 一个 Cloudflare 账号 (免费即可), 一个托管在 Cloudflare 上的域名 (给中控用), 一个 [CZL Connect](https://connect.czl.net) OAuth 应用 (管理后台登录), 以及至少一台装了 Docker 的服务器跑节点。

### 让 Agent 帮你部署

把下面这段整体复制给 Claude Code / Codex / Cursor 等编程 Agent (需要它能执行命令), 按提示补齐尖括号里的值即可:

```text
帮我部署 proxy-go (https://github.com/woodchen-ink/proxy-go): 中控部署到我的 Cloudflare 账号, 节点用 Docker 部署。

我的信息:
- 中控管理域名: <例如 proxy-admin.example.com, 域名需托管在 Cloudflare>
- CZL Connect OAuth: client_id=<...> client_secret=<...>
  (回调地址需要在 CZL Connect 应用里填 https://<中控管理域名>/admin/api/oauth/callback)
- 企业微信群机器人 webhook (可选, 用于节点掉线 / 切换通知): <...>
- 节点服务器: <SSH 地址列表, 或说明我自己去服务器上执行>

步骤:
1. git clone 仓库; 确认本机有 Node.js 20+, 用 `npx wrangler login` 登录 Cloudflare。
2. 构建管理前端: `cd web && npm ci && npm run build` (产物 web/out 会随中控一起发布)。
3. `cd cloudflare-worker && npm ci`; `npx wrangler d1 create proxy-go-data` 拿到 database_id。
4. 复制 sample.wrangler.toml 为 wrangler.toml, 填入 database_id, 把 routes 里的 i-manage.czl.net 换成我的中控管理域名。
5. 生成随机值并用 `npx wrangler secret put <名字>` 写入以下密钥 (值不要打印到日志):
   API_TOKEN (节点令牌, openssl rand -hex 32), CACHE_CLEAR_REMOTE_TOKEN (公开清缓存令牌),
   SESSION_SECRET (openssl rand -base64 48), CRED_KEY (必须是 32 字节 base64: openssl rand -base64 32),
   OAUTH_CLIENT_ID, OAUTH_CLIENT_SECRET, 以及可选的 WECOM_WEBHOOK_URL。
   API_TOKEN 和 CACHE_CLEAR_REMOTE_TOKEN 部署完要告诉我, 后面节点和第三方要用。
6. `npx wrangler deploy`; 打开 https://<中控管理域名>/admin/ 确认能看到登录页。
7. 在每台节点服务器上用仓库里的 docker-compose.yml 部署, 环境变量:
   D1_SYNC_ENABLED=true, D1_SYNC_URL=https://<中控管理域名>, D1_SYNC_TOKEN=<API_TOKEN>, TZ=Asia/Shanghai;
   挂载 ./data:/app/data 持久化 (节点 ID / 配置快照 / 指令游标都在里面)。
8. 节点启动后会自动连上中控。登录后台「节点」页确认每个节点在线, 再到「配置」页添加路径映射。
9. 如需 DNS 故障切换: 在「DNS」页添加 Cloudflare / 腾讯云凭据, 在「节点」页给节点登记回源主机名, 再新建绑定。
```

### 手动部署

**1. 中控**

```bash
git clone https://github.com/woodchen-ink/proxy-go && cd proxy-go
cd web && npm ci && npm run build && cd ..
cd cloudflare-worker && npm ci
npx wrangler login
npx wrangler d1 create proxy-go-data          # 记下 database_id
cp sample.wrangler.toml wrangler.toml         # 填 database_id, 改 routes 为你的管理域名
npx wrangler secret put API_TOKEN              # 节点令牌, 随机 32 字节 hex
npx wrangler secret put CACHE_CLEAR_REMOTE_TOKEN
npx wrangler secret put SESSION_SECRET
npx wrangler secret put CRED_KEY               # openssl rand -base64 32
npx wrangler secret put OAUTH_CLIENT_ID
npx wrangler secret put OAUTH_CLIENT_SECRET
npx wrangler secret put WECOM_WEBHOOK_URL      # 可选
npx wrangler deploy
```

不想装本地环境也可以用仓库的 GitHub Actions: Fork 后在 Secrets 里填 `CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID`, 运行 **Deploy Cloudflare Worker**, 再用 `wrangler secret put` (或 Dashboard → Worker → Settings → Variables) 补上其余密钥, 详见 [cloudflare-worker/README.md](cloudflare-worker/README.md)。

**2. 节点**

```yaml
# docker-compose.yml
services:
  proxy:
    image: ghcr.io/woodchen-ink/proxy-go:latest
    ports:
      - "3336:3336"
    volumes:
      - ./data:/app/data
    environment:
      - TZ=Asia/Shanghai
      - D1_SYNC_ENABLED=true
      - D1_SYNC_URL=https://<中控管理域名>
      - D1_SYNC_TOKEN=<API_TOKEN>
    restart: always
```

节点 ID 默认按出口 IP 生成并固化到 `data/node_id`, 也可以用 `NODE_ID` 指定。节点连中控默认走 `D1_SYNC_URL`, 需要分开时设 `CONTROL_URL`。

**3. 登录后台**: 打开 `https://<中控管理域名>/admin/`, 用 CZL Connect 登录。

**4. 节点更新 (可选)**: 在 fork 的仓库 Settings → Secrets 里加 `CONTROL_URL` (`https://<中控管理域名>`) 与 `CONTROL_API_TOKEN` (与中控 `wrangler secret put RELEASE_TOKEN` 设的值相同, 不要复用 `API_TOKEN`), 之后每次 push main, CI 会发布节点程序并登记到中控, 在后台「节点」页选版本逐个滚动升级, 或打开「新版本自动升级程序」。基础镜像更新用同一页的「滚动重建容器」, 需先在「Dokploy 设置」里登记 Dokploy 地址、API Key 与每个节点对应的应用。节点必须挂载 `data` 卷, 新程序存在 `data/bin`。

## Favicon

环境变量 `FAVICON_URL` 指定一个外部地址即可 (`FAVICON_URL=https://example.com/favicon.ico`); 也可以替换镜像内的 `web/public/favicon.ico`。优先级: `FAVICON_URL` > 本地文件 > 404。

## 远程单 URL 清理缓存

给第三方应用调用的清缓存接口, 部署在中控上, 一次清全部节点。

```bash
curl -X POST 'https://<中控管理域名>/api/cache/clear-url' \
  -H 'Authorization: Bearer <CACHE_CLEAR_REMOTE_TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://your-domain.com/b2/img/logo.png?v=123", "type": "all"}'
```

- `url` 支持完整 URL 或站内路径, 只按路径清理, 忽略 query 与 fragment; `type` 可选 `proxy` / `mirror` / `all` (默认)
- 全部在线节点清理完 (最多等 3 秒) 后返回, `cleared_items` 是各节点之和; 离线节点重连后补执行
- 若后台启用了 CDN provider, 会再异步清理对应完整 URL 的 CDN 缓存; 传站内路径时需额外带 `"host": "your-domain.com"` 才会清 CDN
- 同一路由在每个节点上也可用: 节点会把请求原样转发给中控并补上 host, 老调用方不改地址也能清到全集群

成功响应:

```json
{
  "code": 200,
  "data": {
    "input_url": "https://your-domain.com/b2/img/logo.png?v=123",
    "normalized_url": "/b2/img/logo.png",
    "type": "all",
    "cleared_items": 3,
    "nodes": 3,
    "pending_nodes": 0
  },
  "msg": "cache cleared"
}
```

## 客户端缓存头（Cache-Control）

代理会按**本节点的缓存时长**自动下发 `Cache-Control` 响应头，无需额外配置。

### 取值规则

直接复用路径的「最大缓存时间」(`CacheConfig.max_age`，单位分钟)，换算成秒写入响应头：

| 路径缓存时间 | 下发的响应头 |
|-------------|-------------|
| 30 分钟 | `Cache-Control: public, max-age=1800` |
| 1440 分钟 | `Cache-Control: public, max-age=86400` |

取值为三级回退，与本地缓存的判定完全同源：

1. **路径级** `CacheConfig.max_age`
2. **全局** `Cache.max_age`（路径未配置或为 0 时）
3. **默认值 30 分钟**（全局也缺失或为 0 时）

第 3 级很重要：`CacheManager` 在配置缺失时本来就按 30 分钟缓存，客户端头必须给出同一个数，否则会出现「本地按 30 分钟缓存、却不告诉浏览器缓存多久」的口径不一致。默认值统一定义在 `config.DefaultCacheMaxAge`，两处共用。

`/mirror` 走同样的三级回退，只是第 2 级取 `MirrorCache` 而非 `Cache`。

### 关闭某个路径的缓存

把该路径的「最大缓存时间」设为 **0**，表示这个路径完全不缓存：

- 不写本地磁盘缓存
- 不读已有缓存（改配置之前落盘的旧副本也不再服务）
- 下发 `Cache-Control: no-store, no-cache, must-revalidate`

注意区分「没配」和「配了 0」：

| 配置 | 含义 |
|------|------|
| 路径无 `CacheConfig` | 继承全局配置 |
| 路径 `max_age: 0` | **该路径不缓存** |
| 路径 `max_age: 30` | 缓存 30 分钟 |
| 全局 `Cache.max_age: 0` | 视为漏配，回退默认 30 分钟（**不是**全站禁用） |

全局的 0 刻意不解读为「全站禁用缓存」：它更可能是配置漏填，若按禁用处理，一份不完整的配置会静默关掉所有缓存。要全局关缓存请用缓存管理页的开关。

### 为什么无条件覆盖上游

上游可能是任意网站、GitHub、S3 等，各自的缓存头互不相干且不可控。我们实际只保证缓存 `max_age` 分钟，超过就会回源重取，因此告诉下游的时长应当与之一致——上游说什么不影响我们留多久，也就不该影响我们告诉浏览器留多久。

上游的 `Cache-Control` 与 `Expires` 都会被覆盖/清除。

### 一致性说明

缓存命中（HIT）与未命中（MISS）下发的头完全一致。命中时走 `http.ServeFile`，不经过响应头复制流程，因此单独补齐——否则同一个 URL 会出现「MISS 有缓存头、HIT 没有」，命中后浏览器只能按 `Last-Modified` 做启发式缓存。

## 域名过滤功能

### 功能介绍

新增的域名过滤功能允许你为不同的请求域名配置不同的扩展规则。这在以下场景中非常有用：

1. **多域名服务**: 一个代理服务绑定多个域名（如 a.com 和 b.com）
2. **差异化配置**: 不同域名使用不同的CDN或存储服务
3. **精细化控制**: 根据域名和文件类型组合进行精确路由

### 配置示例

```json
{
  "MAP": {
    "/images": {
      "DefaultTarget": "https://default-cdn.com",
      "ExtensionMap": [
        {
          "Extensions": "jpg,png,webp",
          "Target": "https://a-domain-cdn.com",
          "SizeThreshold": 1024,
          "MaxSize": 2097152,
          "Domains": "a.com",
          "RedirectMode": false
        },
        {
          "Extensions": "jpg,png,webp", 
          "Target": "https://b-domain-cdn.com",
          "SizeThreshold": 1024,
          "MaxSize": 2097152,
          "Domains": "b.com",
          "RedirectMode": true
        },
        {
          "Extensions": "mp4,avi",
          "Target": "https://video-cdn.com",
          "SizeThreshold": 1048576,
          "MaxSize": 52428800
          // 不指定Domains，对所有域名生效
        }
      ]
    }
  }
}
```

### 使用场景

#### 场景1: 多域名图片CDN
```
请求: https://a.com/images/photo.jpg (1MB)
结果: 代理到 https://a-domain-cdn.com/photo.jpg

请求: https://b.com/images/photo.jpg (1MB)  
结果: 302跳转到 https://b-domain-cdn.com/photo.jpg

请求: https://c.com/images/photo.jpg (1MB)
结果: 代理到 https://default-cdn.com/photo.jpg (使用默认目标)
```

#### 场景2: 域名+扩展名组合规则
```
请求: https://a.com/files/video.mp4 (10MB)
结果: 代理到 https://video-cdn.com/video.mp4 (匹配通用视频规则)

请求: https://b.com/files/video.mp4 (10MB)
结果: 代理到 https://video-cdn.com/video.mp4 (匹配通用视频规则)
```

### 配置字段说明

- **Domains**: 逗号分隔的域名列表，指定该规则适用的域名
  - 为空或不设置：匹配所有域名
  - 单个域名：`"a.com"`
  - 多个域名：`"a.com,b.com,c.com"`
- **Extensions**: 文件扩展名（与之前相同）
- **Target**: 目标服务器（与之前相同）
- **SizeThreshold/MaxSize**: 文件大小范围（与之前相同）
- **RedirectMode**: 是否使用302跳转（与之前相同）

### 匹配优先级

1. **域名匹配**: 首先筛选出匹配请求域名的规则
2. **扩展名匹配**: 在域名匹配的规则中筛选扩展名匹配的规则
3. **文件大小匹配**: 根据文件大小选择最合适的规则
4. **目标可用性**: 检查目标服务器是否可访问
5. **默认回退**: 如果没有匹配的规则，使用默认目标

### 日志输出

启用域名过滤后，日志会包含域名信息：

```
[SelectRule] /image.jpg -> 选中规则 (域名: a.com, 文件大小: 1.2MB, 在区间 1KB 到 2MB 之间)
[Redirect] /image.jpg -> 使用选中规则进行302跳转 (域名: b.com): https://b-domain-cdn.com/image.jpg
```

## 原有功能

### 功能作用

用于统一路径管理与 CDN 调度: 一个域名后接多个源 (对象存储 / CDN / 站点静态文件), 通过 proxy-go 做路径级路由, 外层再套 Cloudflare 或 EdgeOne 等 CDN, 优化全球访问。

config里MAP的功能

目前我的主要使用是反代B2, R2, Oracle存储桶之类的. 也可以反代网站静态资源, 可以一并在CDN环节做缓存.

根据config示例作示范

访问https://proxy-go/path1/123.jpg, 实际是访问  https://path1.com/path/path/path/123.jpg
访问https://proxy-go/path2/749.movie, 实际是访问https://path2.com/749.movie

### mirror 固定路由
比较适合接口类的CORS问题

访问https://proxy-go/mirror/https://example.com/path/to/resource

会实际访问https://example.com/path/to/resource



