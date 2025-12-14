# Proxy-Go

A 'simple' reverse proxy server written in Go.

使用方法: https://www.sunai.net/t/topic/165

最新镜像地址: woodchen/proxy-go:latest

## 新版统计仪表盘

![image](https://github.com/user-attachments/assets/0b87863e-5566-4ee6-a3b7-94a994cdd572)

## 图片

![image](https://github.com/user-attachments/assets/99b1767f-9470-4838-a4eb-3ce70bbe2094)

### 仪表统计盘

![image](https://github.com/user-attachments/assets/e09d0eb1-e1bb-435b-8f90-b04bc474477b)

### 配置页

![image](https://github.com/user-attachments/assets/5acddc06-57f5-417c-9fec-87e906dc22af)

### 缓存页

![image](https://github.com/user-attachments/assets/6225b909-c5ff-4374-bb07-c472fbec791d)

## 说明

1. 支持gzip和brotli压缩
2. 不同路径代理不同站点
3. 回源Host修改
4. 大文件使用流式传输, 小文件直接提供
5. 可以按照文件后缀名代理不同站点, 方便图片处理等
6. 适配Cloudflare Images的图片自适应功能, 透传`Accept`头, 支持`format=auto`
7. 支持网页端监控和管理

## 功能特性

- 🚀 **多路径代理**: 根据不同路径代理到不同的目标服务器
- 🔄 **扩展名规则**: 根据文件扩展名和大小智能选择目标服务器
- 🌐 **域名过滤**: 支持根据请求域名应用不同的扩展规则
- 📦 **压缩支持**: 支持Gzip和Brotli压缩
- 🎯 **302跳转**: 支持302跳转模式
- 📊 **缓存管理**: 智能缓存机制提升性能
- 📈 **监控指标**: 内置监控和指标收集
- 🎨 **自定义 Favicon**: 通过环境变量轻松设置自定义 favicon

## Favicon 配置

### 方式 1: 环境变量 (推荐)

通过环境变量 `FAVICON_URL` 指定一个外部 URL，无需修改任何文件：

```yaml
# docker-compose.yml
environment:
  - FAVICON_URL=https://example.com/favicon.ico
```

**优点**:
- ✅ 无需映射本地文件或目录
- ✅ 适合使用预构建 Docker 镜像的用户
- ✅ 可以使用 R2/B2/CDN 上的 favicon

### 方式 2: 本地文件

替换 `web/public/favicon.ico` 文件并重启服务。

**优先级**: 环境变量 `FAVICON_URL` > 本地文件 `web/public/favicon.ico` > 返回 404

详细说明请查看 [favicon/README.md](favicon/README.md)

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

主要是最好有一台国外服务器, 回国又不慢的, 可以反代国外资源, 然后在proxy-go外面套个cloudfront或者Edgeone, 方便国内访问.

config里MAP的功能

目前我的主要使用是反代B2, R2, Oracle存储桶之类的. 也可以反代网站静态资源, 可以一并在CDN环节做缓存.

根据config示例作示范

访问https://proxy-go/path1/123.jpg, 实际是访问  https://path1.com/path/path/path/123.jpg
访问https://proxy-go/path2/749.movie, 实际是访问https://path2.com/749.movie

### mirror 固定路由
比较适合接口类的CORS问题

访问https://proxy-go/mirror/https://example.com/path/to/resource

会实际访问https://example.com/path/to/resource



