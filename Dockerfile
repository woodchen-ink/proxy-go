# 构建后端
FROM alpine:latest

ARG TARGETARCH
WORKDIR /app

COPY proxy-go.${TARGETARCH} /app/proxy-go
COPY web/out /app/web/out

RUN mkdir -p /app/data && \
    chmod +x /app/proxy-go && \
    apk add --no-cache ca-certificates tzdata

EXPOSE 3336
VOLUME ["/app/data"]
ENTRYPOINT ["/app/proxy-go"]
