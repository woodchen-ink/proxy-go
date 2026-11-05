package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// CacheRemoteHandler 节点上的公开清缓存接口 POST /api/cache/clear-url
//
// 清缓存已收归中控 (全部节点 + CDN 一起清): 这里只做透明转发, 让仍在调用节点域名的
// 第三方不改配置也能清到全集群。鉴权 (Bearer CACHE_CLEAR_REMOTE_TOKEN) 由中控校验,
// 请求体原样透传; 站内路径请求补上本次请求的 host, 中控据此拼出 CDN 清理用的完整 URL。
type CacheRemoteHandler struct {
	hubBase func() string
	client  *http.Client
}

func NewCacheRemoteHandler(hubBase func() string) *CacheRemoteHandler {
	return &CacheRemoteHandler{hubBase: hubBase, client: &http.Client{Timeout: 20 * time.Second}}
}

const maxRemoteClearBody = 64 * 1024

func (h *CacheRemoteHandler) ClearCacheByURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRemoteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	base := h.hubBase()
	if base == "" {
		writeRemoteError(w, http.StatusNotFound, "not found")
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRemoteClearBody))
	if err != nil {
		writeRemoteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		writeRemoteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := body["host"]; !ok && r.Host != "" {
		body["host"] = r.Host
	}
	forward, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(base, "/")+"/api/cache/clear-url", bytes.NewReader(forward))
	if err != nil {
		writeRemoteError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", r.Header.Get("Authorization"))

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[RemoteCacheClear] 转发中控失败 ip=%s: %v", iputil.GetClientIP(r), err)
		writeRemoteError(w, http.StatusBadGateway, "failed to clear cache")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxRemoteClearBody))
}

func writeRemoteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "msg": msg})
}
