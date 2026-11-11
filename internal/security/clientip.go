package security

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// 访客真实 IP 的解析 (封禁 / 限流 / 日志 / 统计共用)
//
// 节点通常跑在 Dokploy 的 Traefik 后面, 前面再套 CDN: 访客 → CDN 边缘 → Traefik → 节点。
// 只有"连到我们的那一跳"是可信的, 所以分两步:
//  1. 定位对端: 连接来自本机 / 内网 (Traefik、docker 网关) 时, 对端取 X-Forwarded-For 最右一项
//     (反代追加的、它实际看到的来源); 否则对端就是 RemoteAddr
//  2. 对端落在某个 CDN 的回源网段内, 才采信该 CDN 声明的访客头 (CF-Connecting-IP / EO-Client-IP);
//     否则对端本身就是访客 —— 直连节点的人伪造这些头不起作用
//
// 未配置网段的入口 (CIDRs 为空) 退回旧行为: 对端不属于任何已配网段时仍采信该头。
// 这只为不知道回源网段的 CDN (如 EdgeOne) 保底, 节点 IP 若暴露则可被伪造, 管理端会标出提醒。

// TrustedIngress 一个 CDN 入口: 回源网段 + 它声明访客 IP 的请求头
type TrustedIngress struct {
	Header string
	CIDRs  []string
}

// CloudflareCIDRs Cloudflare 官方回源网段 (https://www.cloudflare.com/ips/), 多年未变; 内置, 不依赖配置
var CloudflareCIDRs = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

type ingressRule struct {
	header string
	tp     *iputil.TrustedProxies // nil = 未配网段 (仅凭请求头)
}

type clientIPResolver struct {
	rules []ingressRule // 配了网段的在前, 未配网段的兜底在后
}

var resolver atomic.Pointer[clientIPResolver]

func init() {
	SetTrustedIngresses(nil)
}

// SetTrustedIngresses 用配置重建解析器 (配置热更新回调里调用); Cloudflare 始终内置在最前
func SetTrustedIngresses(extra []TrustedIngress) {
	all := append([]TrustedIngress{{Header: "CF-Connecting-IP", CIDRs: CloudflareCIDRs}}, extra...)
	var withNets, headerOnly []ingressRule
	for _, in := range all {
		h := strings.TrimSpace(in.Header)
		if h == "" {
			continue
		}
		if len(in.CIDRs) == 0 {
			headerOnly = append(headerOnly, ingressRule{header: h})
			continue
		}
		tp, err := iputil.NewTrustedProxies(h, in.CIDRs...)
		if err != nil {
			continue // 非法网段由管理端校验, 这里跳过而不是让节点起不来
		}
		withNets = append(withNets, ingressRule{header: h, tp: tp})
	}
	resolver.Store(&clientIPResolver{rules: append(withNets, headerOnly...)})
}

// ClientIP 访客真实 IP, 安全判断一律用它 (不要用 iputil.GetClientIP, 它无条件信任各家 CDN 头)
func ClientIP(r *http.Request) string {
	return resolver.Load().clientIP(r)
}

func (c *clientIPResolver) clientIP(r *http.Request) string {
	peer := peerIP(r)
	for _, rule := range c.rules {
		if rule.tp != nil && !rule.tp.Contains(peer) {
			continue
		}
		if rule.tp == nil && c.peerInKnownCDN(peer) {
			continue // 对端已确认是别家 CDN, 不读这家的头
		}
		if v := strings.TrimSpace(r.Header.Get(rule.header)); v != "" && net.ParseIP(v) != nil {
			return v
		}
		if rule.tp != nil {
			return peer // 确认来自该 CDN 但没带头: 退回对端, 不再看别家的头
		}
	}
	return peer
}

func (c *clientIPResolver) peerInKnownCDN(ip string) bool {
	for _, rule := range c.rules {
		if rule.tp != nil && rule.tp.Contains(ip) {
			return true
		}
	}
	return false
}

// peerIP 连到 Traefik / 节点的那一跳; 只有 RemoteAddr 是本机或内网 (反代) 时才看 X-Forwarded-For 最右一项
func peerIP(r *http.Request) string {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	if !isInternal(remote) {
		return remote
	}
	values := r.Header.Values("X-Forwarded-For")
	for i := len(values) - 1; i >= 0; i-- {
		parts := strings.Split(values[i], ",")
		for j := len(parts) - 1; j >= 0; j-- {
			if p := strings.TrimSpace(parts[j]); net.ParseIP(p) != nil {
				return p
			}
		}
	}
	return remote
}

func isInternal(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsLoopback() || p.IsPrivate() || p.IsLinkLocalUnicast())
}

// IsInternalIP 本机 / 内网地址; 解析不出访客时会落到反代或 docker 网关上, 这类地址不能被封禁 (否则等于封全站)
func IsInternalIP(ip string) bool {
	return isInternal(ip)
}
