# 路径统计和缓存管理功能实施总结

## 已完成的后端修改

### 1. 配置结构扩展 (`internal/config/types.go`)
- 为 `PathConfig` 添加了：
  - `Enabled bool` - 是否启用路径映射
  - `CacheConfig *CacheConfig` - 独立缓存配置

### 2. 统计数据模型扩展 (`internal/models/metrics.go`)
- 为 `PathMetrics` 添加了详细统计字段：
  - `Status2xx`, `Status3xx`, `Status4xx`, `Status5xx` - 状态码分类统计
  - `CacheHits`, `CacheMisses` - 缓存命中统计
  - `BytesSaved` - 通过缓存节省的字节数
- 更新了 `PathMetricsJSON` 结构，包含 `CacheHitRate` 计算

### 3. Metrics收集器增强 (`internal/metrics/collector.go`)
- 添加了 `pathStats *RefererStats` 字段用于按路径收集统计
- 扩展了 `RequestMetric` 结构支持缓存信息：
  - `CacheHit bool`
  - `BytesSaved int64`
- 添加了 `RecordRequestWithCache()` 方法
- 在 `updateMetricsBatch()` 中添加了路径统计逻辑：
  - 记录每个路径的请求、错误、字节传输
  - 统计状态码分布（2xx/3xx/4xx/5xx）
  - 记录缓存命中/未命中
- 新增方法：
  - `GetPathStats()` - 获取所有路径统计
  - `GetPathStatByPath(path)` - 获取单个路径统计

### 4. API接口 (`internal/handler/path_stats.go` 新文件)
```go
type PathStatsHandler struct {
    collector *metrics.Collector
}

// GET /admin/api/path-stats - 获取所有路径统计
func (h *PathStatsHandler) GetAllPathStats(w http.ResponseWriter, r *http.Request)
```

### 5. 路由配置
- 修改了 `internal/router/admin_router.go`，添加路径统计路由
- 修改了 `internal/initapp/init.go`，初始化并注册 `PathStatsHandler`

## 已完成的前端修改

### 1. 类型定义扩展 (`web/app/dashboard/config/page.tsx`)
```typescript
interface CacheConfig {
  max_age: number;
  cleanup_tick: number;
  max_cache_size: number;
}

interface PathMapping {
  // ... 现有字段
  Enabled?: boolean;
  CacheConfig?: CacheConfig;
}

interface PathStats {
  path: string;
  request_count: number;
  error_count: number;
  bytes_transferred: number;
  avg_latency: string;
  status_2xx, status_3xx, status_4xx, status_5xx: number;
  cache_hits, cache_misses: number;
  cache_hit_rate: number;
  bytes_saved: number;
}
```

### 2. 状态管理
- 添加了 `pathStats` 状态用于存储路径统计信息
- 在 `fetchConfig()` 中添加了获取路径统计的逻辑

## 剩余工作（需要继续完成）

### 前端UI改进

#### 1. 在路径映射表格中显示统计信息
需要在配置页面的路径映射表格中添加以下列：
- **请求数** (`request_count`)
- **错误率** (`error_count / request_count`)
- **状态码分布** (2xx/3xx/4xx/5xx的比例图表或数字)
- **缓存命中率** (`cache_hit_rate` 显示为百分比)
- **流量统计** (`bytes_transferred` 和 `bytes_saved`)
- **平均延迟** (`avg_latency`)

建议UI实现：
```tsx
{/* 在现有的路径映射卡片中 */}
<div className="space-y-2">
  {Object.entries(config.MAP).map(([path, mapping]) => {
    const stats = pathStats.find(s => s.path === path);
    return (
      <Card key={path}>
        <CardContent className="p-4">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <div className="font-semibold">{path}</div>
              {stats && (
                <div className="flex gap-4 text-sm text-muted-foreground mt-2">
                  <span>请求: {stats.request_count.toLocaleString()}</span>
                  <span>错误率: {((stats.error_count / stats.request_count) * 100).toFixed(2)}%</span>
                  <span>缓存命中: {(stats.cache_hit_rate * 100).toFixed(1)}%</span>
                  <span>延迟: {stats.avg_latency}</span>
                </div>
              )}
            </div>
            {/* 操作按钮 */}
          </div>
        </CardContent>
      </Card>
    );
  })}
</div>
```

#### 2. 添加缓存配置对话框
为每个路径添加缓存配置按钮，点击后弹出对话框：
```tsx
<Dialog>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>配置缓存 - {selectedPath}</DialogTitle>
    </DialogHeader>
    <div className="space-y-4">
      <div>
        <Label>最大缓存时间（分钟）</Label>
        <Input type="number" value={cacheConfig.max_age} />
      </div>
      <div>
        <Label>清理间隔（分钟）</Label>
        <Input type="number" value={cacheConfig.cleanup_tick} />
      </div>
      <div>
        <Label>最大缓存大小（GB）</Label>
        <Input type="number" value={cacheConfig.max_cache_size} />
      </div>
    </div>
  </DialogContent>
</Dialog>
```

#### 3. 移除原有的独立缓存管理页面
原 `web/app/dashboard/cache/page.tsx` 的功能已整合到配置页面中，可以考虑：
- 将该页面改为重定向到配置页面
- 或者保留作为全局缓存概览页面

### /mirror路径作为系统级路径映射

#### 后端修改
需要在配置初始化时自动添加 `/mirror` 路径到 `Config.MAP`：

```go
// internal/config/config.go
func (cm *ConfigManager) LoadConfigFromFile() error {
    // ... 现有加载逻辑

    // 确保 /mirror 路径存在
    if cfg.MAP == nil {
        cfg.MAP = make(map[string]PathConfig)
    }

    // 如果 /mirror 不存在，添加默认配置
    if _, exists := cfg.MAP["/mirror"]; !exists {
        cfg.MAP["/mirror"] = PathConfig{
            DefaultTarget: "mirror",  // 特殊标记
            Enabled:       true,
            CacheConfig: &CacheConfig{
                MaxAge:       30,
                CleanupTick:  5,
                MaxCacheSize: 10,
            },
        }
    }

    // ... 保存配置
}
```

#### 前端修改
在配置页面中：
1. `/mirror` 路径显示为系统路径（特殊样式）
2. 允许用户：
   - 启用/禁用
   - 修改缓存配置
   - 无法删除（显示为灰色或禁用删除按钮）
3. 添加说明文本："此为系统级路径，用于镜像代理功能"

### 缓存清理功能
需要添加按路径清理缓存的API和前端按钮：

#### 后端 (已存在，需调整)
```go
// internal/handler/cache_admin.go 修改
POST /admin/api/cache/clear?path=/path1  // 清理特定路径的缓存
```

#### 前端
在每个路径的操作按钮中添加"清理缓存"按钮：
```tsx
<Button
  variant="outline"
  size="sm"
  onClick={() => clearPathCache(path)}
>
  <Trash2 className="h-4 w-4 mr-1" />
  清理缓存
</Button>
```

## 测试清单

### 后端测试
- [ ] 编译无错误 ✅
- [ ] 启动服务器无错误
- [ ] GET `/admin/api/path-stats` 返回正确的统计数据
- [ ] 缓存命中时正确记录 `CacheHit` 和 `BytesSaved`
- [ ] 不同状态码正确分类到 2xx/3xx/4xx/5xx
- [ ] 配置保存时正确处理 `Enabled` 和 `CacheConfig` 字段

### 前端测试
- [ ] 路径统计数据正确显示
- [ ] 缓存配置对话框正常工作
- [ ] 状态码分布可视化清晰
- [ ] 缓存命中率正确计算和显示
- [ ] /mirror路径显示为系统路径
- [ ] 清理缓存功能正常工作

## 配置示例

```json
{
  "MAP": {
    "/path1": {
      "DefaultTarget": "https://example.com",
      "Enabled": true,
      "CacheConfig": {
        "max_age": 60,
        "cleanup_tick": 10,
        "max_cache_size": 5
      },
      "ExtensionMap": [...]
    },
    "/mirror": {
      "DefaultTarget": "mirror",
      "Enabled": true,
      "CacheConfig": {
        "max_age": 30,
        "cleanup_tick": 5,
        "max_cache_size": 10
      }
    }
  }
}
```

## 架构优化建议（未来）

### 1. 多路径缓存管理器
当前缓存仍使用全局的 `proxyCache` 和 `mirrorCache`。未来可以考虑：
```go
type PathCacheManager struct {
    caches map[string]*cache.CacheManager
    mu     sync.RWMutex
}

func (m *PathCacheManager) GetCache(path string) *cache.CacheManager {
    // 根据路径返回对应的缓存管理器
    // 如果路径有自定义CacheConfig，创建独立缓存
    // 否则使用全局缓存
}
```

### 2. 缓存策略接口
```go
type CacheStrategy interface {
    ShouldCache(req *http.Request, resp *http.Response) bool
    GetTTL(req *http.Request, resp *http.Response) time.Duration
    GenerateKey(req *http.Request) string
}
```

### 3. 统计数据持久化
当前路径统计仅存在内存中，重启后丢失。可以考虑：
- 定期保存到 `data/metrics/path_stats.json`
- 启动时加载历史数据
- 提供数据导出功能

## 下一步操作

1. **完成前端UI** - 按照上述"剩余工作"部分实现前端功能
2. **添加/mirror系统路径** - 后端和前端同时支持
3. **测试** - 运行完整的测试清单
4. **文档更新** - 更新 CLAUDE.md 和用户文档
