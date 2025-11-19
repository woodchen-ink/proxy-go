# 前端更新说明

为了配合后端的性能优化,前端新增了健康检查管理界面。

## 📋 新增功能

### 1. 健康检查页面 (`/dashboard/health`)

#### 功能特性
- **实时监控**: 每5秒自动刷新健康状态
- **可视化展示**:
  - 颜色编码 (绿色=健康, 红色=不健康)
  - 成功率百分比显示
  - 响应时间统计
- **目标管理**:
  - 查看所有目标服务器状态
  - 重置单个目标的健康状态
  - 查看详细错误信息

#### 数据展示
1. **健康摘要**:
   - 总体健康度
   - 总目标数
   - 健康目标数
   - 不健康目标数

2. **目标详情表格**:
   - 健康状态 (绿/红指示灯)
   - 目标URL
   - 成功率 (百分比 + 成功/失败次数)
   - 平均延迟
   - 总请求数 & 失败数
   - 连续失败次数
   - 上次检查时间
   - 上次成功时间
   - 操作按钮 (重置)

3. **说明卡片**:
   - 健康判定标准
   - 成功率颜色说明

#### 颜色规范
遵循项目设计规范 (CLAUDE.md):
- **健康**: `#518751` (成功绿)
- **警告**: `#ecec70` (突出显示黄)
- **不健康**: `#b85e48` (危险红)
- **强调**: `#C08259` (主强调色)
- **背景**: `#F4E8E0` (已选择色)

---

### 2. 导航菜单更新 (`components/nav.tsx`)

新增 "健康检查" 导航链接:
```tsx
<Link href="/dashboard/health">
  健康检查
</Link>
```

导航顺序:
1. 仪表盘
2. 配置
3. 缓存
4. 安全
5. **健康检查** (新增)

---

### 3. 仪表盘首页更新 (`/dashboard`)

#### 新增健康状态卡片
在仪表盘首页顶部添加快捷健康检查卡片:

**显示内容**:
- 总体健康度 (百分比)
- 总目标数
- 健康目标数
- 不健康目标数
- "查看详情" 链接 → 跳转到健康检查页面

**背景颜色**:
- 健康度 ≥90%: 浅绿色 (`#F4E8E0`)
- 健康度 70-89%: 浅黄色 (`#fcfce0`)
- 健康度 <70%: 浅红色 (`#fce8e8`)

**自动刷新**:
- 每5秒自动更新健康状态
- 不影响主界面性能

---

## 📁 文件清单

### 新增文件 (1个)
- `web/app/dashboard/health/page.tsx` - 健康检查页面

### 修改文件 (2个)
- `web/components/nav.tsx` - 添加导航链接
- `web/app/dashboard/page.tsx` - 添加健康摘要卡片

---

## 🎨 UI设计特点

### 1. 响应式布局
- 移动端: 单列布局
- 桌面端: 多列网格布局
- 表格: 横向滚动支持

### 2. 交互设计
- **悬停效果**: 表格行悬停显示背景色
- **链接样式**: 使用项目主题色 (`#C08259`)
- **按钮**:
  - 主要操作: 使用主题色
  - 次要操作: outline 样式
  - 危险操作: 使用危险色 (`#b85e48`)

### 3. 确认对话框
使用 shadcn/ui 的 `AlertDialog` 组件:
```tsx
<AlertDialog>
  <AlertDialogTitle>确认重置</AlertDialogTitle>
  <AlertDialogDescription>...</AlertDialogDescription>
  <AlertDialogCancel>取消</AlertDialogCancel>
  <AlertDialogAction>确认重置</AlertDialogAction>
</AlertDialog>
```

### 4. 时间格式化
智能显示相对时间:
- < 60秒: "X秒前"
- < 60分钟: "X分钟前"
- < 24小时: "X小时前"
- ≥ 24小时: 完整日期时间

---

## 🔧 API 集成

### 1. 获取健康状态
```typescript
GET /admin/api/health/status
Headers: { Authorization: `Bearer ${token}` }

Response:
{
  "targets": [...],
  "summary": {
    "total_targets": 5,
    "healthy_targets": 4,
    "unhealthy_targets": 1,
    "overall_health": 80.0
  }
}
```

### 2. 重置目标
```typescript
POST /admin/api/health/reset
Headers: {
  Authorization: `Bearer ${token}`,
  Content-Type: "application/json"
}
Body: { "url": "https://example.com" }

Response:
{
  "success": true,
  "message": "Target health reset successfully",
  "url": "https://example.com"
}
```

---

## 📊 数据流

```
用户访问 /dashboard/health
    ↓
组件挂载,调用 fetchHealthStatus()
    ↓
每5秒自动刷新 (setInterval)
    ↓
展示健康状态列表
    ↓
用户点击 "重置" 按钮
    ↓
显示确认对话框
    ↓
调用 handleReset(url)
    ↓
POST /admin/api/health/reset
    ↓
显示成功提示
    ↓
重新获取健康状态
```

---

## 🚀 构建和部署

### 开发模式
```bash
cd web
npm run dev
# 访问 http://localhost:13001/dashboard/health
```

### 生产构建
```bash
cd web
npm run build
# 输出到 web/out 目录
```

### 构建结果
```
Route (app)                              Size     First Load JS
├ ○ /dashboard/health                    4.59 kB         130 kB
```

---

## ⚠️ 注意事项

### 1. 兼容性
- 仅在健康检查功能启用时显示数据
- 如果后端不支持健康检查API,会优雅降级 (不显示)

### 2. 性能优化
- 健康状态刷新间隔: 5秒 (避免过于频繁)
- 仪表盘健康摘要: 5秒刷新 (与健康页面一致)
- 主仪表盘指标: 1秒刷新 (保持实时性)

### 3. 错误处理
- API调用失败: 显示错误提示,不影响界面
- 未授权: 自动跳转到登录页
- 网络错误: 显示重试按钮

---

## 📖 使用指南

### 查看健康状态
1. 登录管理后台
2. 点击导航栏 "健康检查"
3. 查看所有目标服务器的健康状态

### 理解健康指标
- **绿色指示灯**: 服务健康,连续成功 ≥2次
- **红色指示灯**: 服务不健康,连续失败 ≥3次
- **成功率**: (总请求数 - 失败请求数) / 总请求数
- **平均延迟**: 所有成功请求的平均响应时间

### 重置健康状态
1. 找到需要重置的目标
2. 点击 "重置" 按钮
3. 确认对话框中点击 "确认重置"
4. 该目标的健康历史将被清除

### 快速查看
在仪表盘首页可以看到健康摘要卡片:
- 总体健康度
- 健康/不健康目标数量
- 点击 "查看详情" 进入完整健康检查页面

---

## 🎯 下一步建议

### 可选增强功能
1. **图表展示**:
   - 健康度趋势图
   - 响应时间历史图
   - 成功率柱状图

2. **告警配置**:
   - 不健康目标数量阈值告警
   - 总体健康度低于阈值告警
   - 邮件/webhook通知

3. **批量操作**:
   - 批量重置多个目标
   - 导出健康报告
   - 筛选和排序功能

4. **更多统计**:
   - 按时间段查看健康历史
   - 目标服务器分组显示
   - 健康状态变化日志

---

**最后更新**: 2025-11-19
**版本**: v1.0.0
