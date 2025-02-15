# Proxy-Go

A 'simple' reverse proxy server written in Go.

使用方法: https://q58.club/t/topic/165?u=wood

## 图片

### 仪表统计盘
![image](https://github.com/user-attachments/assets/40083aea-8fc4-4bb3-93c8-736ff410883d)
![image](https://i-aws.czl.net/r2/original/1X/43172322d9423fd86b537363d10b11ae0b3fd678.png)

### 配置可在线修改并热重载
![image](https://github.com/user-attachments/assets/53517eaf-2bbd-462c-b1ff-5f8f85764436)

### 缓存查看和控制
![image](https://i-aws.czl.net/r2/original/1X/ab943fcb964aef56a2da6dcfc7b4b21b105d8d02.png)


## 说明

1. 支持gzip和brotli压缩, 在`config.json`中配置
2. 不同路径代理不同站点
3. 回源Host修改
4. 大文件使用流式传输, 小文件直接提供
5. 可以按照文件后缀名代理不同站点, 方便图片处理等
6. 适配Cloudflare Images的图片自适应功能, 透传`Accept`头, 支持`format=auto`
7. 支持网页端监控和管理



