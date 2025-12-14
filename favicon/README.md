# Favicon 设置指南

## 方式 1: 使用环境变量 (推荐 - 适用于 Docker 部署)

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
- ✅ 修改后重启即可生效

**示例**:
```bash
# 使用 R2 存储的 favicon
FAVICON_URL=https://cdn.example.com/my-favicon.ico

# 使用 GitHub 仓库中的 favicon
FAVICON_URL=https://raw.githubusercontent.com/user/repo/main/favicon.ico
```

---

## 方式 2: 替换本地文件 (适用于自建镜像)

如果你自己构建 Docker 镜像或直接运行，可以替换本地文件：

### 使用方法

1. 将你的 favicon 文件重命名为 `favicon.ico`
2. 替换 `web/public/favicon.ico` 文件
3. 重启 proxy-go 服务

### 示例

```bash
# 替换 favicon 文件
cp your-favicon.ico web/public/favicon.ico

# 重启服务
docker-compose restart
```

---

## 优先级

系统按以下优先级查找 favicon：

1. **环境变量 `FAVICON_URL`** (最高优先级)
2. **本地文件 `web/public/favicon.ico`**
3. **返回 404** (无 favicon)

---

## 注意事项

- **推荐尺寸**: 16x16, 32x32, 48x48 像素
- **支持格式**: `.ico` (推荐), `.png`, `.jpg`, `.svg`
- **缓存时间**: 1 年（修改后需要清除浏览器缓存）

---

## ⚠️ 已废弃的目录

`favicon/` 目录已不再使用，请使用上述新方式配置。