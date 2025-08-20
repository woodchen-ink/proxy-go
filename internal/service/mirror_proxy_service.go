package service

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/utils"
	"strings"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// MirrorProxyRequest 镜像代理请求
type MirrorProxyRequest struct {
	OriginalRequest *http.Request
	ActualURL       string
	ParsedURL       *url.URL
}

// MirrorProxyResponse 镜像代理响应
type MirrorProxyResponse struct {
	StatusCode      int
	Headers         http.Header
	Body            io.ReadCloser
	ContentLength   int64
	FromCache       bool
	CacheKey        string
}

// MirrorProxyResult 镜像代理处理结果
type MirrorProxyResult struct {
	Success       bool
	StatusCode    int
	ErrorMessage  string
	BytesWritten  int64
	Duration      time.Duration
	FromCache     bool
	ActualURL     string
}

type MirrorProxyService struct {
	client *http.Client
	cache  *cache.CacheManager
}

func NewMirrorProxyService(client *http.Client, cache *cache.CacheManager) *MirrorProxyService {
	return &MirrorProxyService{
		client: client,
		cache:  cache,
	}
}

// ExtractTargetURL 从镜像路径提取实际URL
func (s *MirrorProxyService) ExtractTargetURL(r *http.Request) (*MirrorProxyRequest, error) {
	actualURL := strings.TrimPrefix(r.URL.Path, "/mirror/")
	if actualURL == "" || actualURL == r.URL.Path {
		return nil, fmt.Errorf("invalid URL")
	}

	if r.URL.RawQuery != "" {
		actualURL += "?" + r.URL.RawQuery
	}

	// 解析URL
	parsedURL, err := url.Parse(actualURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}

	// 确保有scheme
	if parsedURL.Scheme == "" {
		actualURL = "https://" + actualURL
		parsedURL, _ = url.Parse(actualURL)
	}

	return &MirrorProxyRequest{
		OriginalRequest: r,
		ActualURL:       actualURL,
		ParsedURL:       parsedURL,
	}, nil
}

// CheckCache 检查缓存
func (s *MirrorProxyService) CheckCache(req *MirrorProxyRequest) (*cache.CacheItem, bool, bool) {
	if req.OriginalRequest.Method != http.MethodGet || s.cache == nil {
		return nil, false, false
	}

	cacheKey := s.cache.GenerateCacheKey(req.OriginalRequest)
	item, hit, notModified := s.cache.Get(cacheKey, req.OriginalRequest)
	return item, hit, notModified
}

// CreateProxyRequest 创建代理请求
func (s *MirrorProxyService) CreateProxyRequest(req *MirrorProxyRequest) (*http.Request, error) {
	proxyReq, err := http.NewRequest(req.OriginalRequest.Method, req.ActualURL, req.OriginalRequest.Body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// 复制原始请求的header
	s.copyHeaders(proxyReq.Header, req.OriginalRequest.Header)

	// 设置必要的请求头
	scheme := req.ParsedURL.Scheme
	host := req.ParsedURL.Host

	proxyReq.Header.Set("Origin", fmt.Sprintf("%s://%s", scheme, host))
	proxyReq.Header.Set("Referer", fmt.Sprintf("%s://%s/", scheme, host))
	
	if ua := req.OriginalRequest.Header.Get("User-Agent"); ua != "" {
		proxyReq.Header.Set("User-Agent", ua)
	} else {
		proxyReq.Header.Set("User-Agent", "Mozilla/5.0")
	}
	
	proxyReq.Header.Set("Host", host)
	proxyReq.Host = host

	return proxyReq, nil
}

// ExecuteRequest 执行代理请求
func (s *MirrorProxyService) ExecuteRequest(proxyReq *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("error forwarding request: %v", err)
	}
	return resp, nil
}

// ProcessResponse 处理响应并写入缓存
func (s *MirrorProxyService) ProcessResponse(req *MirrorProxyRequest, resp *http.Response, w http.ResponseWriter) (int64, error) {
	// 复制响应头
	s.copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Proxy-Go-Cache-HIT", "0")
	w.WriteHeader(resp.StatusCode)

	var written int64
	var err error

	// 如果是GET请求且响应成功，使用TeeReader同时写入缓存
	if req.OriginalRequest.Method == http.MethodGet && resp.StatusCode == http.StatusOK && s.cache != nil {
		written, err = s.processWithCache(req, resp, w)
	} else {
		written, err = io.Copy(w, resp.Body)
	}

	if err != nil && !s.isConnectionClosed(err) {
		return written, fmt.Errorf("error writing response: %v", err)
	}

	return written, nil
}

// processWithCache 处理带缓存的响应
func (s *MirrorProxyService) processWithCache(req *MirrorProxyRequest, resp *http.Response, w http.ResponseWriter) (int64, error) {
	cacheKey := s.cache.GenerateCacheKey(req.OriginalRequest)
	
	if cacheFile, err := s.cache.CreateTemp(cacheKey, resp); err == nil {
		defer cacheFile.Close()
		teeReader := io.TeeReader(resp.Body, cacheFile)
		written, err := io.Copy(w, teeReader)
		if err == nil {
			s.cache.Commit(cacheKey, cacheFile.Name(), resp, written)
		}
		return written, err
	}
	
	return io.Copy(w, resp.Body)
}

// copyHeaders 复制HTTP头部，过滤hop-by-hop头部
func (s *MirrorProxyService) copyHeaders(dst, src http.Header) {
	// 基础 hop-by-hop 头部
	hopHeaders := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Proxy-Connection":    true,
		"Te":                  true,
		"Trailer":             true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}

	// 处理 Connection 头部中的额外 hop-by-hop 头部
	if connectionHeader := src.Get("Connection"); connectionHeader != "" {
		for _, header := range strings.Split(connectionHeader, ",") {
			hopHeaders[strings.TrimSpace(header)] = true
		}
	}

	// 复制非 hop-by-hop 头部
	for name, values := range src {
		if !hopHeaders[name] {
			dst[name] = values
		}
	}
}

// isConnectionClosed 检查是否为连接关闭错误
func (s *MirrorProxyService) isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "use of closed network connection")
}

// CreateLogEntry 创建日志条目
func (s *MirrorProxyService) CreateLogEntry(req *MirrorProxyRequest, statusCode int, duration time.Duration, bytesWritten int64) string {
	return fmt.Sprintf("| %-6s | %3d | %12s | %15s | %10s | %-30s | %s",
		req.OriginalRequest.Method, statusCode, duration,
		iputil.GetClientIP(req.OriginalRequest), utils.FormatBytes(bytesWritten),
		utils.GetRequestSource(req.OriginalRequest), req.ActualURL)
}