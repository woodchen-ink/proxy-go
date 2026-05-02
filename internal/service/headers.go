package service

import (
	"net/http"
	"net/textproto"
	"strings"
)

// hopByHopHeaders 是 RFC 7230 §6.1 定义的 hop-by-hop 头部，不能转发到上游/下游
// 用 textproto.CanonicalMIMEHeaderKey 形式作为 key，匹配 http.Header 的 key 形式
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// securityHeadersToStrip 是来自上游、不应透传给客户端的安全相关头部
var securityHeadersToStrip = map[string]struct{}{
	"Content-Security-Policy":             {},
	"Content-Security-Policy-Report-Only": {},
	"X-Content-Security-Policy":           {},
	"X-Webkit-Csp":                        {},
}

// copyFilteredHeaders 把 src 中除 hop-by-hop（含 Connection 中列出的额外条目）和 stripExtra
// 中指定的头部之外的所有头部复制到 dst。stripExtra 可为 nil。
//
// 实现要点：
//   - 全局 hopByHopHeaders / securityHeadersToStrip 只读，避免每请求构建 map
//   - Connection 头部里临时声明的额外 hop-by-hop 头部用一个轻量 slice 处理（通常 0~2 项）
func copyFilteredHeaders(dst, src http.Header, stripExtra map[string]struct{}) {
	// 解析 Connection 头部中声明的额外 hop-by-hop 头部
	var extraHop []string
	if connectionHeader := src.Get("Connection"); connectionHeader != "" {
		for _, header := range strings.Split(connectionHeader, ",") {
			h := strings.TrimSpace(header)
			if h != "" {
				extraHop = append(extraHop, textproto.CanonicalMIMEHeaderKey(h))
			}
		}
	}

	for name, values := range src {
		if _, isHop := hopByHopHeaders[name]; isHop {
			continue
		}
		if stripExtra != nil {
			if _, strip := stripExtra[name]; strip {
				continue
			}
		}
		if len(extraHop) > 0 {
			skip := false
			for _, h := range extraHop {
				if h == name {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		dst[name] = values
	}
}
