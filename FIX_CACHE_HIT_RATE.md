# 修复缓存命中率统计问题

## 问题描述

路径映射的缓存命中率始终显示为 0，即使缓存系统正常工作。

## 根本原因

在记录请求统计时，所有的请求都使用了普通的 `RecordRequest()` 方法，而不是 `RecordRequestWithCache()` 方法。这导致：

1. `CacheHits` 和 `CacheMisses` 计数器从未被更新
2. 缓存命中率计算公式 `cacheHitRate = cacheHits / (cacheHits + cacheMisses)` 始终返回 0（因为分子分母都是0）
3. `BytesSaved`（节省的字节数）统计也始终为0

## 修复方案

将所有的请求记录从 `RecordRequest()` 改为 `RecordRequestWithCache()`，并传递正确的缓存状态：

### 1. 缓存命中场景（3处修改）

**文件**: `internal/handler/proxy.go` 和 `internal/handler/mirror_proxy.go`

**修改前**:
```go
collector.RecordRequest(r.URL.Path, http.StatusOK, time.Since(start), item.Size, iputil.GetClientIP(r), r)
```

**修改后**:
```go
// 缓存命中，节省的字节数等于文件大小
collector.RecordRequestWithCache(r.URL.Path, http.StatusOK, time.Since(start), item.Size, iputil.GetClientIP(r), r, true, item.Size)
```

**修改位置**:
- `internal/handler/proxy.go:369` - handleCacheHit 方法
- `internal/handler/proxy.go:364` - handleCacheHit 方法 (304 Not Modified)
- `internal/handler/mirror_proxy.go:186` - handleCacheHit 方法
- `internal/handler/mirror_proxy.go:180` - handleCacheHit 方法 (304 Not Modified)

### 2. 缓存未命中场景（3处修改）

**文件**: `internal/handler/proxy.go` 和 `internal/handler/mirror_proxy.go`

**修改前**:
```go
collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), written, iputil.GetClientIP(r), r)
```

**修改后**:
```go
// 缓存未命中
collector.RecordRequestWithCache(r.URL.Path, resp.StatusCode, time.Since(start), written, iputil.GetClientIP(r), r, false, 0)
```

**修改位置**:
- `internal/handler/proxy.go:328` - ServeHTTP 主流程
- `internal/handler/proxy.go:422` - handleMissedCache 方法
- `internal/handler/mirror_proxy.go:150` - ServeHTTP 主流程

## 缓存统计逻辑

统计数据在 `internal/metrics/collector.go:826-832` 中更新：

```go
// 更新缓存统计
if m.CacheHit {
    pathMetrics.CacheHits.Add(1)
    pathMetrics.BytesSaved.Add(m.BytesSaved)
} else {
    pathMetrics.CacheMisses.Add(1)
}
```

缓存命中率在 `internal/models/metrics.go:100-106` 中计算：

```go
cacheHits := p.CacheHits.Load()
cacheMisses := p.CacheMisses.Load()
totalCache := cacheHits + cacheMisses
var cacheHitRate float64
if totalCache > 0 {
    cacheHitRate = float64(cacheHits) / float64(totalCache)
}
```

## 测试验证

修复后，需要验证：

1. **缓存命中时**: `cache_hit_rate` 应该增加
2. **缓存未命中时**: 请求仍然被正确记录
3. **混合场景**: 命中率应该准确反映实际的缓存效率（例如 10次请求中7次命中 = 70%）
4. **节省字节数**: `bytes_saved` 应该累计所有缓存命中节省的带宽

## 影响范围

- ✅ 修复了路径级别的缓存命中率统计
- ✅ 修复了缓存节省字节数的统计
- ✅ 同时适用于 Proxy 和 Mirror 两种代理模式
- ✅ 不影响其他统计指标（请求数、错误数、延迟等）

## 注意事项

- 304 Not Modified 响应也计入缓存命中，因为它确实节省了带宽传输
- `bytesSaved` 在304响应时设为 `item.Size`，因为客户端没有下载文件内容
- 历史统计数据可能仍为0（因为旧数据没有记录缓存状态），新请求才会正确统计
