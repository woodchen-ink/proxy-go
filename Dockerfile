FROM --platform=$TARGETPLATFORM alpine:latest

WORKDIR /app

COPY proxy-go.$TARGETARCH /app/proxy-go

RUN mkdir -p /app/data && \
    chmod +x /app/proxy-go && \
    apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

EXPOSE 80
VOLUME ["/app/data"]
ENTRYPOINT ["/app/proxy-go"]
