package security

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	SetTrustedIngresses([]TrustedIngress{{Header: "EO-Client-IP", CIDRs: []string{"43.100.0.0/16"}}})
	defer SetTrustedIngresses(nil)

	cases := []struct {
		name    string
		remote  string
		xff     []string
		headers map[string]string
		want    string
	}{
		{"CF 直连节点", "162.158.1.1:443", nil, map[string]string{"CF-Connecting-IP": "8.8.8.8"}, "8.8.8.8"},
		{"CF 经 Traefik (覆盖 XFF)", "172.18.0.2:1", []string{"162.158.1.1"}, map[string]string{"CF-Connecting-IP": "8.8.8.8"}, "8.8.8.8"},
		{"CF 经 Traefik (追加 XFF, 访客伪造前缀)", "172.18.0.2:1", []string{"1.1.1.1, 8.8.8.8, 162.158.1.1"}, map[string]string{"CF-Connecting-IP": "8.8.8.8"}, "8.8.8.8"},
		{"直连伪造 CF 头", "9.9.9.9:1", nil, map[string]string{"CF-Connecting-IP": "1.2.3.4"}, "9.9.9.9"},
		{"经 Traefik 直连伪造 CF / EO 头", "172.18.0.2:1", []string{"9.9.9.9"}, map[string]string{"CF-Connecting-IP": "1.2.3.4", "EO-Client-IP": "5.6.7.8"}, "9.9.9.9"},
		{"EO 网段内采信 EO 头", "172.18.0.2:1", []string{"43.100.2.3"}, map[string]string{"EO-Client-IP": "7.7.7.7"}, "7.7.7.7"},
		{"EO 边缘伪造 CF 头无效", "43.100.2.3:1", nil, map[string]string{"CF-Connecting-IP": "1.2.3.4", "EO-Client-IP": "7.7.7.7"}, "7.7.7.7"},
		{"CF 边缘没带头时退回边缘 IP", "162.158.1.1:1", nil, map[string]string{"EO-Client-IP": "7.7.7.7"}, "162.158.1.1"},
		{"非法头值", "162.158.1.1:1", nil, map[string]string{"CF-Connecting-IP": "not-an-ip"}, "162.158.1.1"},
		{"内网无 XFF", "172.17.0.1:1", nil, nil, "172.17.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := ClientIP(r); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestClientIPHeaderOnlyIngress(t *testing.T) {
	// 未配网段的入口 (旧行为保底): 对端不属于已知 CDN 时采信该头; 对端是 CF 时不读
	SetTrustedIngresses([]TrustedIngress{{Header: "EO-Client-IP"}})
	defer SetTrustedIngresses(nil)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "43.100.2.3:1"
	r.Header.Set("EO-Client-IP", "7.7.7.7")
	if got := ClientIP(r); got != "7.7.7.7" {
		t.Fatalf("header-only ingress: got %s", got)
	}
	r.RemoteAddr = "162.158.1.1:1"
	r.Header.Set("CF-Connecting-IP", "8.8.8.8")
	if got := ClientIP(r); got != "8.8.8.8" {
		t.Fatalf("CF peer should use CF header: got %s", got)
	}
}
