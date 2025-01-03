FROM alpine:latest

ARG TARGETARCH
WORKDIR /app

COPY proxy-go.${TARGETARCH} /app/proxy-go

RUN mkdir -p /app/data && \
    chmod +x /app/proxy-go && \
    apk add --no-cache ca-certificates tzdata

EXPOSE 80
VOLUME ["/app/data"]
ENTRYPOINT ["/app/proxy-go"]
