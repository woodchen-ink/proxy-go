# 缓存命中率和路径统计重置功能修复

## 问题总结

### 问题1：缓存命中率始终显示为 0

**根本原因**：所有请求记录都使用了 `RecordRequest()` 方法，而不是 `RecordRequestWithCache()` 方法，导致缓存命中和未命中的计数器从未被更新。

**影响范围**：
- 路径统计中的缓存命中率显示为 0
- 缓存节省字节数统计为 0
- 无法准确评估缓存效果

**修复方案**：
将所有请求记录改为使用 `RecordRequestWithCache()` 并传递正确的缓存状态：

1. **缓存命中时**：
   - 参数：`cacheHit=true, bytesSaved=item.Size`
   - 修改位置：`internal/handler/proxy.go` 和 `internal/handler/mirror_proxy.go`

2. **缓存未命中时**：
   - 参数：`cacheHit=false, bytesSaved=0`
   - 修改位置：主代理流程和重试流程

**修改文件**：
- `internal/handler/proxy.go` (4处)
- `internal/handler/mirror_proxy.go` (2处)

### 问题2：路径统计重置功能不生效

**根本原因**：
1. **后端问题**：重置接口只重置了精确匹配的路径，但实际统计数据按具体请求路径存储（例如 `/b2/file1.jpg`），而前端传递的是路径前缀（例如 `/b2`）
2. **前端问题**：重置后调用了错误的刷新函数（`fetchConfig` 而不是刷新统计数据）

**修复方案**：

#### 后端修复

修改 `ResetPathStats` 为**路径前缀匹配**模式：

```go
// 修改前：只重置精确匹配的路径
if stats, ok := c.pathStats.Load(path); ok {
    // 重置该路径...
}

// 修改后：重置所有匹配前缀的路径
c.pathStats.Range(func(key string, stats *models.PathMetrics) bool {
    if strings.HasPrefix(key, pathPrefix) {
        // 确保完整路径段匹配
        if len(key) == len(pathPrefix) || key[len(pathPrefix)] == '/' {
            // 重置所有计数器...
        }
    }
    return true
})
```

**精确匹配逻辑**：
- `/b2` 会匹配 `/b2`, `/b2/file.jpg`, `/b2/dir/file.jpg`
- `/b2` 不会匹配 `/b2x` 或 `/b2345`（确保路径段完整）

#### 前端修复

修改 `handleResetPathStats` 函数，重置后正确刷新统计数据：

```typescript
// 修改前
await fetchConfig()  // 错误：刷新配置而不是统计

// 修改后
const statsResponse = await fetch("/admin/api/path-stats", {
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  }
})
if (statsResponse.ok) {
  const statsData = await statsResponse.json()
  setPathStats(statsData.path_stats || [])
}
```

## 测试验证

### 缓存命中率测试

1. **访问资源** - 第一次访问某个路径的资源（缓存未命中）
   - 预期：`cache_misses` +1，命中率保持低值

2. **再次访问** - 第二次访问相同资源（缓存命中）
   - 预期：`cache_hits` +1，命中率上升

3. **查看统计** - 在配置页面查看路径统计
   - 预期：缓存命中率正确显示（例如 50%, 70%）

### 路径统计重置测试

1. **查看当前统计** - 记录当前的请求数、缓存命中率等数据

2. **点击重置按钮** - 在统计卡片右上角点击"重置"按钮

3. **验证结果**：
   - ✅ 后端日志显示：`[Collector] 已重置路径前缀 /xxx 的统计，共 N 条`
   - ✅ 前端显示：toast提示"统计数据已重置"
   - ✅ 统计数据归零：总请求数、缓存命中数、流量等全部变为0
   - ✅ 持久化：`data/path_stats.json` 中对应路径的统计数据清零

4. **新请求测试** - 访问该路径的资源
   - 预期：统计数据从0开始重新累计

## 日志说明

### 正常运行日志

```
[Collector] 已重置路径前缀 /cdnjs 的统计，共 X 条
[PathStatsStorage] 路径统计数据已保存: 20592 条路径记录
```

**解释**：
- 第一行：成功重置了 `/cdnjs` 路径前缀下的 X 条统计记录
- 第二行：将所有路径统计（20592条）持久化到磁盘
  - 注意：这个数字是**全部路径**的统计记录数，不是重置的数量
  - 包含已重置的路径（计数器为0）和其他路径（保持原值）

## 相关文件

### 后端修改

1. **`internal/handler/proxy.go`**
   - 主代理流程缓存统计记录 (4处)

2. **`internal/handler/mirror_proxy.go`**
   - Mirror代理缓存统计记录 (2处)

3. **`internal/metrics/collector.go`**
   - 添加 `strings` 导入
   - 重构 `ResetPathStats` 为路径前缀匹配

4. **`internal/handler/path_stats.go`**
   - `ResetPathStats` API处理器

5. **`internal/router/admin_router.go`**
   - 注册重置API路由

### 前端修改

1. **`web/app/dashboard/config/PathStatsCard.tsx`**
   - 添加重置按钮
   - 实现重置逻辑和状态管理

2. **`web/app/dashboard/config/PathMappingItem.tsx`**
   - 传递 `onResetStats` prop

3. **`web/app/dashboard/config/page.tsx`**
   - 实现 `handleResetPathStats` 函数
   - 修复重置后的数据刷新逻辑

## API接口

### POST /admin/api/path-stats/reset

重置指定路径前缀的统计数据

**请求**：
```json
{
  "path": "/b2"
}
```

**响应**：
```json
{
  "success": true,
  "message": "路径统计已重置"
}
```

**行为**：
- 重置所有以 `/b2` 开头的路径统计
- 立即持久化到 `data/path_stats.json`
- 如果配置了S3，自动同步到云端

## 注意事项

1. **路径前缀匹配**：重置 `/b2` 会重置所有以 `/b2` 开头的路径，包括 `/b2/file1.jpg`, `/b2/dir/file2.jpg` 等

2. **不可恢复**：重置操作不可撤销，统计数据将永久清除

3. **持久化延迟**：重置后立即持久化到本地，但S3同步可能有几秒延迟

4. **配置保留**：只重置统计数据，路径映射配置（URL、扩展名规则、缓存配置等）不受影响

5. **多节点环境**：如果配置了S3，重置操作会通过持久化同步到其他节点

## 后续优化建议

- [ ] 添加重置前确认对话框（防止误操作）
- [ ] 显示重置影响的路径数量
- [ ] 支持批量重置多个路径
- [ ] 记录重置历史（谁在何时重置了哪个路径）
