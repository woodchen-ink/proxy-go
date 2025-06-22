# Favicon 自定义设置

## 使用方法

1. 将你的 favicon 文件重命名为 `favicon.ico`
2. 放置在这个 `favicon` 目录中
3. 重启 proxy-go 服务

## 支持的文件格式

- `.ico` 文件（推荐）
- `.png` 文件（需要重命名为 favicon.ico）
- `.jpg/.jpeg` 文件（需要重命名为 favicon.ico）
- `.svg` 文件（需要重命名为 favicon.ico）

## 注意事项

- 文件必须命名为 `favicon.ico`
- 推荐尺寸：16x16, 32x32, 48x48 像素
- 如果没有放置文件，将返回 404（浏览器会使用默认图标）

## 示例

```bash
# 将你的 favicon 文件复制到这个目录
cp your-favicon.ico ./favicon/favicon.ico

# 重启服务
docker-compose restart
```

现在访问 `http://your-domain.com/favicon.ico` 就会显示你的自定义 favicon 了！ 