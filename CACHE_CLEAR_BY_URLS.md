# 按 URL 列表清理缓存功能

## 功能概述

新增了精确清理指定 URL 缓存的功能，支持一次性清理多个 URL 的缓存。

## 使用方法

### 前端界面

1. 进入**配置页面** → **缓存管理** Tab
2. 点击页面顶部的 **"按 URL 清理"** 按钮
3. 在弹出的对话框中输入需要清理的 URL 列表
4. 点击 **"确认清理"** 完成操作

### 输入格式

支持两种输入格式：

**换行符分隔**（推荐）：
```
/b2/img/photo.jpg
/oracle/file.pdf
/b2/video/demo.mp4
```

**逗号分隔**：
```
/b2/img/photo.jpg, /oracle/file.pdf, /b2/video/demo.mp4
```

**混合格式**（也支持）：
```
/b2/img/photo1.jpg,/b2/img/photo2.jpg
/oracle/file.pdf
```

### API 接口

**端点**：`POST /admin/api/cache/clear-by-urls`

**请求体**：
```json
{
  "type": "all",
  "urls": [
    "/b2/img/photo.jpg",
    "/oracle/file.pdf",
    "/b2/video/demo.mp4"
  ]
}
```

**响应**：
```json
{
  "success": true,
  "cleared_items": 3,
  "message": "Cleared 3 cache items for 3 URLs"
}
```

**参数说明**：
- `type`: 缓存类型，可选值：`"proxy"`, `"mirror"`, `"all"`（默认 `"all"`）
- `urls`: URL 列表数组

## 实现细节

### 后端实现

1. **CacheManager.ClearCacheByURLs()**
   - 支持批量 URL 精确匹配
   - 自动规范化 URL（去除尾部斜杠）
   - 同时清理内存和磁盘缓存

2. **CacheService.ClearCacheByURLs()**
   - 支持清理 proxy、mirror 或全部缓存
   - 返回清理的缓存项数量

3. **API Handler**
   - 路径：`/admin/api/cache/clear-by-urls`
   - 需要认证（Bearer Token）
   - 验证输入参数

### 前端实现

1. **对话框组件**
   - 使用 shadcn/ui Dialog 组件
   - Textarea 支持多行输入
   - 实时验证输入

2. **URL 解析**
   - 支持换行符和逗号分隔
   - 自动过滤空行
   - Trim 空白字符

3. **用户反馈**
   - 清理中显示加载状态
   - 成功/失败 Toast 提示
   - 自动刷新统计数据

## 使用场景

### 场景 1：清理特定图片缓存

某些图片更新后，需要强制刷新缓存：

```
/b2/img/logo.png
/b2/img/banner.jpg
```

### 场景 2：清理失效资源

批量清理已删除或失效的资源缓存：

```
/oracle/old-file-1.pdf
/oracle/old-file-2.pdf
/b2/deprecated/video.mp4
```

### 场景 3：测试环境调试

开发时需要频繁清理特定文件缓存：

```
/b2/test/sample.jpg
```

## 与其他清理方式的对比

| 功能 | 清理范围 | 使用场景 |
|------|---------|---------|
| **清理所有缓存** | 全部缓存 | 重大更新、测试 |
| **按路径前缀清理** | `/b2` 下所有文件 | 清理某个存储桶 |
| **按 URL 列表清理** | 精确匹配的文件 | 清理特定文件（本功能） |

## 注意事项

1. **URL 格式**：
   - 必须是完整的请求路径（如 `/b2/img/photo.jpg`）
   - 支持带或不带尾部斜杠（会自动规范化）
   - 区分大小写

2. **匹配规则**：
   - 精确匹配（不是前缀匹配）
   - `/b2/img/photo.jpg` 只会清理该文件，不会清理 `/b2/img/photo.jpg.bak`

3. **清理效果**：
   - 清理内存中的缓存键
   - 删除对应的磁盘缓存文件
   - 不影响其他缓存

## 技术栈

- **后端**：Go (GORM, sync.Map)
- **前端**：Next.js + shadcn/ui + Tailwind CSS
- **组件**：Dialog, Textarea, Button, Toast

## 代码位置

- 后端：
  - [internal/cache/manager.go](internal/cache/manager.go#L823) - `ClearCacheByURLs()`
  - [internal/service/cache_service.go](internal/service/cache_service.go#L110) - Service 层
  - [internal/handler/cache_admin.go](internal/handler/cache_admin.go#L193) - API Handler
  - [internal/router/admin_router.go](internal/router/admin_router.go#L37) - 路由注册

- 前端：
  - [web/app/dashboard/config/components/CacheManagement.tsx](web/app/dashboard/config/components/CacheManagement.tsx#L273) - UI 组件
