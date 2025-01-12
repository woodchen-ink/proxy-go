# Proxy-Go

A 'simple' reverse proxy server written in Go.

使用方法: https://q58.pro/t/topic/165?u=wood

## 说明

1. 支持gzip和brotli压缩, 在`config.json`中配置
2. 不同路径代理不同站点
3. 回源Host修改
4. 大文件使用流式传输, 小文件直接提供
5. 可以按照文件后缀名代理不同站点, 方便图片处理等
6. 适配Cloudflare Images的图片自适应功能, 透传`Accept`头, 支持`format=auto`
7. 支持metrics监控, 在`/metrics/ui`查看, 具体可以看帖子里写的用法



