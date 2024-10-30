# Proxy-Go

A simple reverse proxy server written in Go.

使用方法: https://q58.org/t/topic/165?u=wood

## 说明

1. 支持gzip和brotli压缩, 在`config.json`中配置
2. 不同路径代理不同站点
3. 回源Host修改
4. 大文件使用流式传输, 小文件直接提供


## TIPS

写的比较潦草, 希望有能力的同学帮忙完善优化一下

## Configuration

Create a `config.json` file in the `data` directory:

```json
{
  "MAP":{
      "/path1": "https://path1.com/path/path/path",
      "/path2": "https://path2.com"
    }
}
```


