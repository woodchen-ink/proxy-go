# 节点镜像: 只含代理进程; 管理前端随中控 Worker 发布 (cloudflare-worker/)
FROM alpine:latest

ARG TARGETARCH
WORKDIR /app

COPY proxy-go.${TARGETARCH} /app/proxy-go

RUN mkdir -p /app/data && \
    chmod +x /app/proxy-go && \
    apk add --no-cache ca-certificates tzdata

EXPOSE 3336
VOLUME ["/app/data"]
ENTRYPOINT ["/app/proxy-go"]
