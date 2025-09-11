# 配置云端同步功能

✅ **已实现** - Proxy-Go现在支持S3配置同步功能，可以方便多个节点共享一套配置。

## 功能特性

- ✅ **完整目录同步**: 自动同步整个data目录下的所有JSON配置文件
- ✅ **开机时智能同步**: 启动时比对远程和本地文件版本，自动选择最新版本
- ✅ **定时同步**: 每10分钟自动同步一次完整目录
- ✅ **配置变更触发**: 保存配置时快速同步主配置文件
- ✅ **多文件支持**: 同步所有配置和统计数据文件
- ✅ **版本控制**: 基于文件修改时间的智能版本管理
- ✅ **错误处理**: 完善的重试机制和错误日志
- ✅ **分层同步**: 支持快速配置同步和完整目录同步

## 配置方法

### 1. 复制环境变量配置文件
```bash
cp .env.sync.example .env
```

### 2. 编辑环境变量配置
```bash
# S3存储桶名称 (必需)
SYNC_S3_BUCKET=your-proxy-config-bucket

# AWS地区 (必需)
SYNC_S3_REGION=us-east-1

# 访问密钥ID (必需)
SYNC_S3_ACCESS_KEY_ID=your-access-key-id

# 访问密钥 (必需)
SYNC_S3_SECRET_ACCESS_KEY=your-secret-access-key

# S3端点 (可选，默认使用AWS S3)
# SYNC_S3_ENDPOINT=https://s3.amazonaws.com

# 是否使用Path Style URL (可选，默认false)
SYNC_S3_USE_PATH_STYLE=false

# 配置和统计数据在S3中的保存路径前缀 (可选)
# 最终保存路径: {SYNC_CONFIG_PATH}/config.json 和 {SYNC_CONFIG_PATH}/metrics.json
SYNC_CONFIG_PATH=data/proxy-go
```

### 3. 支持的存储提供商

#### AWS S3
```bash
SYNC_S3_ENDPOINT=https://s3.amazonaws.com
SYNC_S3_BUCKET=your-bucket
SYNC_S3_REGION=us-east-1
SYNC_S3_ACCESS_KEY_ID=AKIA...
SYNC_S3_SECRET_ACCESS_KEY=...
SYNC_CONFIG_PATH=data/proxy-go
```

#### MinIO
```bash
SYNC_S3_ENDPOINT=https://your-minio-endpoint.com
SYNC_S3_USE_PATH_STYLE=true
SYNC_S3_BUCKET=proxy-config
SYNC_S3_REGION=us-east-1
SYNC_S3_ACCESS_KEY_ID=your-key
SYNC_S3_SECRET_ACCESS_KEY=your-secret
SYNC_CONFIG_PATH=data/proxy-go
```

#### 阿里云OSS
```bash
SYNC_S3_ENDPOINT=https://oss-cn-hangzhou.aliyuncs.com
SYNC_S3_BUCKET=your-bucket
SYNC_S3_REGION=cn-hangzhou
SYNC_S3_ACCESS_KEY_ID=your-key
SYNC_S3_SECRET_ACCESS_KEY=your-secret
SYNC_CONFIG_PATH=data/proxy-go
```

#### 腾讯云COS
```bash
SYNC_S3_ENDPOINT=https://cos.ap-guangzhou.myqcloud.com
SYNC_S3_BUCKET=your-bucket
SYNC_S3_REGION=ap-guangzhou
SYNC_S3_ACCESS_KEY_ID=your-key
SYNC_S3_SECRET_ACCESS_KEY=your-secret
SYNC_CONFIG_PATH=data/proxy-go
```

## 同步的文件

系统会自动同步以下文件：

### 主配置文件
- `data/config.json` - 主要的代理配置

### 缓存配置文件  
- `data/cache/config.json` - 普通缓存配置
- `data/mirror_cache/config.json` - 镜像缓存配置

### 统计数据文件
- `data/metrics/status_codes.json` - HTTP状态码统计
- `data/metrics/latency_distribution.json` - 响应时间分布
- `data/metrics/path_stats.json` - 路径访问统计
- `data/metrics/referer_stats.json` - 引用来源统计
- `data/metrics/metrics.json` - 基础指标数据

### 静态资源文件
- `favicon/favicon.ico` - 网站图标文件

## 工作原理

### 启动时同步流程
1. **环境准备**: 加载环境变量和初始化基础目录
2. **同步服务初始化**: 在配置加载前初始化同步服务
3. **获取最新配置**: 执行一次完整同步，下载云端最新配置
4. **配置管理器初始化**: 使用已同步的最新配置初始化程序
5. **启动定时同步**: 开启10分钟定时同步任务

### 运行时同步机制
6. **智能文件过滤**: 基于文件后缀自动过滤，只同步配置文件，排除无后缀的缓存项
7. **多目录扫描**: 同时扫描data目录和favicon目录
8. **版本比较**: 基于文件修改时间比较本地和远程版本
9. **智能选择**: 自动选择最新版本的文件进行同步
10. **配置回调**: 配置更新时触发快速配置同步
11. **缓存项保护**: 自动排除无后缀名的缓存数据项，只同步配置文件

## 使用成本分析

使用S3存储同步配置的成本很低：
- **每10分钟同步**: 144次/天（完整目录扫描）
- **配置变更同步**: 约10-20次/天（快速配置同步）
- **文件数量**: 约8-10个文件（配置+统计+favicon）
- **总API调用**: 约300-400次/天，仍远低于1000次限制
- **存储成本**: 所有文件总计约50KB，费用可忽略

## 日志输出

启用同步后，您会看到类似的启动日志：
```
[Init] .env文件加载成功
[Init] 正在初始化同步服务...
[Sync] Sync service initialized successfully
[Init] 正在同步最新配置...
[Sync] Starting full directory sync...
[DirectorySync] Starting directory sync: data
[DirectorySync] Downloaded: config.json
[DirectorySync] Downloaded: cache/config.json
[DirectorySync] Directory sync completed: uploaded 0, downloaded 2 files
[DirectorySync] Starting directory sync: favicon
[DirectorySync] Downloaded: favicon.ico
[DirectorySync] Directory sync completed: uploaded 0, downloaded 1 files
[Sync] Full directory sync completed successfully
[Init] 配置同步完成
[Init] 正在加载配置...
[Config] 配置管理器初始化成功
[Sync] Sync manager started
```

## 故障排除

### 1. 同步服务未启动
```
[Sync] Sync service disabled (no S3 config)
```
**解决**: 检查环境变量配置是否正确

### 2. S3连接失败
```
Warning: Failed to initialize sync service: failed to test S3 connection
```
**解决**: 检查S3凭据、端点和网络连接

### 3. 权限错误
**解决**: 确保S3访问密钥有读写指定存储桶的权限

### 4. 缓存项被同步
如果发现无后缀名的缓存数据被意外同步：
- 检查缓存目录下是否有不应该同步的文件
- 系统会自动排除无后缀名的文件，只同步 `.json`、`.ico`、`.png`、`.svg` 文件

## 安全建议

- 使用专用的S3存储桶存放配置
- 为同步功能创建专门的IAM用户
- 限制IAM权限仅访问指定存储桶
- 定期轮换访问密钥

---

## 技术实现详情

### 架构设计

使用`pkg/sync`包实现云端同步功能：

- **类型定义** (`types.go`): 定义同步相关的数据结构和接口
- **配置管理** (`config.go`): 环境变量配置解析和验证
- **S3客户端** (`s3_client.go`): 基于aws-sdk-go-v2的S3操作实现
- **同步管理器** (`manager.go`): 核心同步逻辑和版本控制
- **目录同步器** (`directory_sync.go`): 完整目录结构同步功能
- **适配器** (`adapters.go`): 配置和统计数据的读写适配
- **服务包装** (`service.go`): 全局服务管理和生命周期控制

### 环境变量支持

- ✅ S3端点配置
- ✅ 存储桶名称
- ✅ 地区设置
- ✅ 对象版本管理
- ✅ 访问密钥配置
- ✅ Path Style URL支持
- ✅ 自定义保存路径

### 集成方式

同步功能已完全集成到主程序启动流程：
- **预同步启动**: 在配置加载前执行同步，确保使用最新配置
- **智能适配**: ConfigAdapter能够在配置管理器未初始化时直接从文件加载
- **双重同步**: 启动时完整同步 + 运行时快速配置同步
- **配置回调**: 配置更新时自动触发同步
- **优雅关闭**: 程序关闭时停止同步服务