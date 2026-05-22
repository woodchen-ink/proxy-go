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

	// cacheKey 是延迟生成的缓存键，每次请求最多生成一次
	cacheKey    cache.CacheKey
	cacheKeySet bool
}

// MirrorProxyResponse 镜像代理响应
type MirrorProxyResponse struct {
	StatusCode    int
	Headers       http.Header
	Body          io.ReadCloser
	ContentLength int64
	FromCache     bool
	CacheKey      string
}

// MirrorProxyResult 镜像代理处理结果
type MirrorProxyResult struct {
	Success      bool
	StatusCode   int
	ErrorMessage string
	BytesWritten int64
	Duration     time.Duration
	FromCache    bool
	ActualURL    string
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

	// 防御性编程：修复 Traefik v3.5.2新版本导致的 URL 问题
	// 当检测到 https:/ 或 http:/ 时，自动补全为 https:// 或 http://
	if strings.HasPrefix(actualURL, "https:/") && !strings.HasPrefix(actualURL, "https://") {
		actualURL = strings.Replace(actualURL, "https:/", "https://", 1)
	} else if strings.HasPrefix(actualURL, "http:/") && !strings.HasPrefix(actualURL, "http://") {
		actualURL = strings.Replace(actualURL, "http:/", "http://", 1)
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
//
// Mirror 模式面向任意上游 URL 透传, 没有路径级 PathConfig, 因此始终走默认的 URL-only 缓存键策略,
// 不启用 CF Images 智能键。需要按格式分桶请改走带 PathConfig 的反代路径。
func (s *MirrorProxyService) CheckCache(req *MirrorProxyRequest) (*cache.CacheItem, bool, bool) {
	if req.OriginalRequest.Method != http.MethodGet || s.cache == nil {
		return nil, false, false
	}

	cacheKey := s.getOrBuildCacheKey(req)
	item, hit, notModified := s.cache.Get(cacheKey, req.OriginalRequest, false)
	return item, hit, notModified
}

// getOrBuildCacheKey 从 MirrorProxyRequest 取缓存键，首次调用时生成并复用
func (s *MirrorProxyService) getOrBuildCacheKey(req *MirrorProxyRequest) cache.CacheKey {
	if !req.cacheKeySet {
		req.cacheKey = s.cache.GenerateCacheKey(req.OriginalRequest, false)
		req.cacheKeySet = true
	}
	return req.cacheKey
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
	w.Header().Set("CZL-Proxy-Cache-HIT", "0")
	w.WriteHeader(resp.StatusCode)

	var written int64
	var err error

	// 如果是GET请求且响应成功，使用TeeReader同时写入缓存
	if req.OriginalRequest.Method == http.MethodGet && resp.StatusCode == http.StatusOK && s.cache != nil {
		written, err = s.processWithCache(req, resp, w)
	} else {
		// 🚀 零拷贝优化: 使用 buffer pool 复用缓冲区
		buf := cache.GetBuffer(32 * 1024)
		defer cache.PutBuffer(buf)
		written, err = io.CopyBuffer(w, resp.Body, buf)
	}

	if err != nil && !s.isConnectionClosed(err) {
		return written, fmt.Errorf("error writing response: %v", err)
	}

	return written, nil
}

// processWithCache 处理带缓存的响应
func (s *MirrorProxyService) processWithCache(req *MirrorProxyRequest, resp *http.Response, w http.ResponseWriter) (int64, error) {
	cacheKey := s.getOrBuildCacheKey(req)

	if cacheFile, err := s.cache.CreateTemp(cacheKey, resp); err == nil {
		// 🚀 零拷贝优化: 使用 buffer pool 复用缓冲区
		buf := cache.GetBuffer(32 * 1024)
		defer cache.PutBuffer(buf)

		teeReader := io.TeeReader(resp.Body, cacheFile)
		written, err := io.CopyBuffer(w, teeReader, buf)

		// 🔧 修复: 确保文件完全写入并同步到磁盘后再关闭和提交
		// 1. 先同步文件内容到磁盘
		if syncErr := cacheFile.Sync(); syncErr != nil && err == nil {
			// 如果同步失败，记录错误但继续（不影响客户端响应）
			// 这里不设置 err，因为客户端已经收到了数据
		}

		// 2. 关闭文件，确保所有缓冲区都被刷新
		closeErr := cacheFile.Close()

		// 3. 只有在写入成功且文件正确关闭的情况下才提交缓存
		if err == nil && closeErr == nil {
			// 异步提交缓存，不阻塞当前请求处理
			fileName := cacheFile.Name()
			respClone := *resp // 创建响应的浅拷贝
			go func() {
				s.cache.Commit(cacheKey, fileName, &respClone, written)
			}()
		} else {
			// 如果关闭失败，尝试再次关闭
			if closeErr != nil {
				cacheFile.Close()
			}
		}

		return written, err
	}

	// 🚀 零拷贝优化: 使用 buffer pool 复用缓冲区
	buf := cache.GetBuffer(32 * 1024)
	defer cache.PutBuffer(buf)
	return io.CopyBuffer(w, resp.Body, buf)
}

// copyHeaders 复制HTTP头部，过滤hop-by-hop头部
func (s *MirrorProxyService) copyHeaders(dst, src http.Header) {
	copyFilteredHeaders(dst, src, nil)
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
