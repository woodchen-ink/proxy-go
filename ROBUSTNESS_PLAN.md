# Proxy-Go 健壮性优化计划

> 调研日期: 2026-05-02
> 范围: 后端性能、并发安全、内存泄漏、安全防护、前端鲁棒性
> 调研方法: 三路并行 Explore，第二轮深度验证甄别误报

## 优先级总览

| 等级 | 数量 | 含义 |
|------|------|------|
| P0 | 4 | 数据丢失、内存泄漏、安全漏洞，必须修 |
| P1 | 6 | 影响稳定性或体验，建议本轮修 |
| P2 | 5 | 代码质量/防御性，可顺手修 |
| P3 | 4 | 长期改进，本轮可不做 |

---

## P0: 必须修复（数据/安全/资源）

### P0-1: OAuth code 与用户信息泄漏到日志
- **位置**: `internal/service/auth_service.go:340`, `:474`
- **问题**: 340 行打印 OAuth `code`（短期凭据），474 行打印用户信息原始响应（含邮箱等 PII）
- **风险**: 日志被采集/外泄即等同凭据/隐私泄漏
- **修法**:
  - 删除 340 行 code 字段，只保留 redirect_uri
  - 474 行降级为只打印长度或 user_id

### P0-2: 指标 channel 静默丢事件
- **位置**: `internal/metrics/collector.go:282`（buffer=10000）, `:326-330`（默认丢弃）
- **问题**: 高负载下事件被静默丢弃，缓存命中率/路径统计失真，且无可观测性
- **修法**:
  - 增加 `dropped_count atomic.Uint64` 计数
  - 在 `/admin/api/metrics` 暴露 dropped 数
  - 高水位告警（>90% 容量时 log warn）

### P0-3: IP 封禁 errorCounts 无界泄漏
- **位置**: `internal/security/rate_limiter.go:127-160`
- **问题**: 每个独立 IP 进 sync.Map 后永不删除，封禁过期/计数重置后条目仍在
- **风险**: 长期运行 + 扫描攻击 → 内存持续增长
- **修法**: 在已有 cleanup 任务中扩展，删除 `lastTime > 24h` 的 errorCounts 条目

### P0-4: 后台 goroutine 无停止信号
- **位置**: `internal/metrics/collector.go` 多处（4 个无停止 for 循环）
- **问题**: 进程 SIGTERM 时无法优雅退出，metrics 持久化可能写一半
- **修法**: 仿照 IPBanManager 加 `stopChan`，main.go shutdown 时调用 `Stop()`

---

## P1: 强烈建议（稳定性/体验）

### P1-1: 前端 dashboard 1s 轮询无退避
- **位置**: `web/app/dashboard/page.tsx:98`
- **问题**: 后端 5xx 时仍每秒打一次，叠加多个 dashboard tab 会雪崩
- **修法**: setInterval → setTimeout 链式 + 失败计数指数退避（1s → 2s → 4s → 最大 30s）

### P1-2: 前端保存中按钮未禁用
- **位置**: `web/app/dashboard/config/page.tsx`（编辑/删除/添加按钮）
- **问题**: "正在自动保存..." 时用户继续编辑，可能导致请求乱序
- **修法**: 各操作按钮加 `disabled={saving}`

### P1-3: 配置同步无防抖（goroutine 风暴）
- **位置**: `pkg/sync/service.go:198-207`
- **问题**: 用户连续点击 enable 开关，每次都 `go func()` 上传到 D1
- **修法**: 在 callback 内加 5s 合并窗口（time.AfterFunc + atomic 标记 pending）

### P1-4: 指标批处理无最大 age
- **位置**: `internal/metrics/collector.go:729-750`
- **问题**: 流量稀疏时 batch 永远填不满，1s ticker 兜底但批量大小无上限
- **修法**: 增加 `maxBatchAge = 500ms` 和明确的容量上限退化路径

### P1-5: bandwidth stats 锁内 O(n)
- **位置**: `internal/metrics/collector.go:578-608`
- **问题**: 写锁内做 map 遍历 + time.Parse，每请求一次
- **修法**: 把"找最旧 key"逻辑改为维护单独的 oldest timestamp，或用环形 buffer

### P1-6: NaN 通过缓存配置写入
- **位置**: `web/app/dashboard/config/components/PathCacheConfigDialog.tsx:166`
- **问题**: `parseFloat("abc") || 0` 处理"abc"得 0，但中间态如"."、"-"会得 NaN 入 state
- **修法**: 抽 `safeParseNumber(v, fallback)` 工具，统一拦 NaN

---

## P2: 顺手修（防御性）

### P2-1: SSRF — favicon URL 未校验
- **位置**: `internal/router/router.go:42`
- **问题**: 管理员可设 `FaviconURL=http://127.0.0.1:6379/`，从外部触发内网探测
- **修法**: 校验 scheme=https，主机不在私有段（10/8、172.16/12、192.168/16、127/8、169.254/16）
- **注**: 影响面小（只有管理员可改），但属于纵深防御

### P2-2: 请求体大小无限制
- **位置**: `internal/handler/proxy.go`, mirror 同
- **问题**: 上传无界 → 内存爆炸（虽然有 buffer pool，但客户端持续发数据仍消耗）
- **修法**: `r.Body = http.MaxBytesReader(w, r.Body, 100<<20)` 默认 100MB，可配置

### P2-3: 路径重命名缺实时校验
- **位置**: `web/app/dashboard/config/page.tsx`（刚加的功能）
- **问题**: 不以 / 开头要等点保存才报错
- **修法**: Input onChange 时同步校验，红框 + 文字提示，禁用保存按钮

### P2-4: security 历史 limit 无 AbortController
- **位置**: `web/app/dashboard/security/page.tsx:93,129`
- **问题**: 快速切换 limit 可能旧请求覆盖新请求结果
- **修法**: useEffect 内 new AbortController()，cleanup 调 abort

### P2-5: 后台 goroutine 无 panic recover
- **位置**: `internal/metrics/collector.go` 各 `go func()`
- **问题**: 任一 panic 整个 goroutine 静默死亡，无日志
- **修法**: 抽 `safeGo(name, fn)` helper，统一 defer recover + log

---

## P3: 长期改进（本轮可不做）

### P3-1: X-Forwarded-For 信任链
- 现状：无条件信任，攻击者可伪造 IP 绕过封禁/污染日志
- 改造：加 `TrustedProxies` 配置，仅在来自可信代理时采信 XFF
- 推迟原因：需要新增配置项 + 部署文档，影响面大

### P3-2: 全局 fetch monkey-patch 重构
- 现状：layout.tsx 改写 `window.fetch` + 各页面又自带 401 处理（重复）
- 改造：统一 fetch 工具 hook，移除全局污染
- 推迟原因：动静大，目前能跑

### P3-3: 配置并发写最后写入获胜
- 现状：两个管理员同时保存，后到的覆盖前到的
- 改造：版本号或 ETag 乐观锁
- 推迟原因：实际用户数量极少，发生概率低

### P3-4: 死代码清理
- `internal/cache/cache.go` 的旧 Cache struct 未实例化
- `web/app/dashboard/config/components/PathDialogForm.tsx` 未引用
- `web/app/dashboard/config/PathMappingItem.tsx` 未引用
- 删除即可，但需确认无外部引用

---

## 已验证的"误报"（无需修复）

- ❌ `proxy.go:313-317` resp.Body 未关闭：Go http 契约保证 err != nil 时 resp 为 nil
- ❌ 配置自动保存的"stale closure"：useCallback 依赖正确，重新创建会刷新
- ❌ localStorage token XSS：同源限制下风险低，且无 inline script 注入面
- ❌ `internal/cache/cache.go` 的清理 goroutine 泄漏：该 struct 未被使用

---

## 推荐执行顺序

**第一波（核心数据/安全，约 1.5h）**
1. P0-1 OAuth 日志脱敏
2. P0-3 IP ban 内存泄漏
3. P0-2 指标丢弃可观测性
4. P0-4 goroutine 优雅停止

**第二波（稳定性，约 1.5h）**
5. P1-1 前端轮询退避
6. P1-2 保存中禁用按钮
7. P1-3 配置同步合并
8. P1-4/5 metrics 批处理与锁优化

**第三波（防御与体验，约 1h）**
9. P2-1 SSRF favicon 校验
10. P2-3 路径校验实时反馈
11. P2-5 panic recover

**第四波（视情况）**
- P3 系列按需

---

## 进度跟踪

### 第一波（P0）— 已完成 ✅

- [x] **P0-1 OAuth 日志脱敏** (2026-05-02)
  - `internal/handler/auth.go:76` 删除 state/code/full URL 打印
  - `internal/service/auth_service.go:474` 用户响应只打 size
  - `internal/service/auth_service.go:319` invalid state 不再回显 state 值
  - 第一轮调研声称 340 行打印 code 是误报（仅打印 redirect_uri）
- [x] **P0-2 指标丢弃可观测性** (2026-05-02)
  - 新增 `Collector.droppedMetrics` atomic 计数
  - 两处 `default` 分支改调 `recordDrop()`，每 1000 次丢弃打告警
  - GetStats 暴露 `dropped_metrics` / `metrics_chan_capacity` / `metrics_chan_pending`
- [x] **P0-3 IP ban 并发安全修复** (2026-05-02)
  - errorRecord 增加 `sync.Mutex` 保护字段
  - RecordError / banIP / cleanup 三处加锁
  - 第一轮调研声称"errorCounts 永不清理"是误报（cleanup 已做），真问题是数据竞争
- [x] **P0-4 goroutine 优雅停止 + panic recover** (2026-05-02)
  - 4 个后台 goroutine（consistencyChecker / cleanupTask / asyncMetricsUpdater / persistenceTask）全部加 stopChan + WaitGroup + recover
  - asyncMetricsUpdater 退出前会排空 channel 残留指标
  - persistenceTask 退出前最后保存一次路径统计
  - main.go shutdown 调 `collector.Stop(5*time.Second)`

构建验证：`go build ./...` ✅、`go vet ./...` ✅

### 第二波（P1）— 已完成 ✅

- [x] **P1-1 前端轮询指数退避** (2026-05-02)
  - dashboard 改为 setTimeout 链式，失败 → 1/2/4/8/16/30s 退避封顶
  - 同时增加 toast 节流（30s 抑制窗口），避免错误弹窗刷屏
- [x] **P1-2 保存中禁用按钮** (2026-05-02)
  - 编辑/删除/新增/保存等按钮全部加 `disabled={saving}`
- [x] **P1-3 配置同步防抖合并** (2026-05-02)
  - `pkg/sync/service.go` ConfigSyncCallback 改为 3s 抖动窗口
  - 新增 `FlushConfigSync()` 在 main.go shutdown 时强制立即同步
  - 解决用户连续点击开关导致 goroutine 风暴
- [x] **P1-4 metrics 批处理** — 验证为误报
  - 实际有 1s ticker 兜底，最长延迟 1s，且 size=1000 上限存在
- [x] **P1-5 bandwidth key 跨年冲突** (2026-05-02)
  - key 格式 `"01-02 15:04"` → `"2006-01-02 15:04"`，字典序==时间序
  - 顺便去掉锁内的 time.Parse，用 string 直接比较
  - 上一轮"O(n) 锁内"是误报：history 上限 5 项，开销可忽略
- [x] **P1-6 NaN 安全解析** (2026-05-02)
  - `PathCacheConfigDialog.tsx:166` parseFloat 加 `Number.isFinite` 守卫
  - `time-input.tsx:84` parseInt 同步加守卫

构建验证：`go build` ✅、`go vet` ✅、`tsc --noEmit` ✅

### 第三波（P2）— 已完成 ✅

- [x] **P2-1 favicon URL SSRF 防御** (2026-05-02)
  - 新增 `validateFaviconURL()`：仅允许 http/https，拒绝私有/回环/链路本地 IP
  - 拒绝 localhost/.local/.internal 主机名
  - 增加 10s 超时 client + 5MB body 限制
- [x] **P2-2 admin API 请求体大小限制** (2026-05-02)
  - `admin_router.go` 给 POST/PUT/PATCH 加 `MaxBytesReader(5MB)`
  - 代理路径未限制（保留大文件代理能力）
- [x] **P2-3 路径实时校验** (2026-05-02)
  - 编辑路径输入框：空/不以 / 开头/与现有路径冲突 → 即时红框提示
  - 保存按钮校验失败时禁用
- [x] **P2-4 security AbortController** (2026-05-02)
  - `fetchData` 接收 AbortSignal，useEffect cleanup 调 abort
  - 切换 historyLimit 时取消上一次请求，避免乱序覆盖
- [x] P2-5 goroutine panic recover — 已随 P0-4 完成

### 还未做（P3 长期）

- [ ] X-Forwarded-For 信任链配置
- [ ] 全局 fetch monkey-patch 重构
- [ ] 配置并发写乐观锁
- [ ] 死代码清理（旧 Cache struct、PathDialogForm.tsx 等）
