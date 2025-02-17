# Proxy-Go

A 'simple' reverse proxy server written in Go.

使用方法: https://q58.club/t/topic/165?u=wood

## 图片

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



