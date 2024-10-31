package utils

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

func GetClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// 获取请求来源
func GetRequestSource(r *http.Request) string {
	referer := r.Header.Get("Referer")
	if referer != "" {
		return fmt.Sprintf(" (from: %s)", referer)
	}
	return ""
}

func FormatBytes(bytes int64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}
