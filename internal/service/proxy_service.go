package service

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"strings"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// ProxyRequest 代理请求结构
type ProxyRequest struct {
	OriginalRequest *http.Request
	MatchedPrefix   string
	PathConfig      config.PathConfig
	TargetPath      string
	StartTime       time.Time
}

// ProxyResponse 代理响应结构
type ProxyResponse struct {
	StatusCode      int
	Headers         http.Header
	Body            io.ReadCloser
	ContentLength   int64
	FromCache       bool
	AltTarget       bool
	CacheKey        string
}

// ProxyResult 代理处理结果
type ProxyResult struct {
	Success       bool
	StatusCode    int
	ErrorMessage  string
	BytesWritten  int64
	Duration      time.Duration
	FromCache     bool
	AltTarget     bool
	TargetURL     string
}

type ProxyService struct {
	client          *http.Client
	cache           *cache.CacheManager
	ruleService     *RuleService
	redirectService *RedirectService
}

func NewProxyService(client *http.Client, cache *cache.CacheManager, ruleService *RuleService) *ProxyService {
	redirectService := NewRedirectService(ruleService)
	return &ProxyService{
		client:          client,
		cache:           cache,
		ruleService:     ruleService,
		redirectService: redirectService,
	}
}

// CheckCache 检查缓存
func (s *ProxyService) CheckCache(req *ProxyRequest) (*cache.CacheItem, bool, bool) {
	if req.OriginalRequest.Method != http.MethodGet || s.cache == nil {
		return nil, false, false
	}

	cacheKey := s.cache.GenerateCacheKey(req.OriginalRequest)
	item, hit, notModified := s.cache.Get(cacheKey, req.OriginalRequest)
	return item, hit, notModified
}

// CheckRedirect 检查是否需要重定向
func (s *ProxyService) CheckRedirect(req *ProxyRequest, w http.ResponseWriter) bool {
	result := s.redirectService.HandleRedirect(req.OriginalRequest, req.PathConfig, req.TargetPath, s.client)
	if result.ShouldRedirect {
		http.Redirect(w, req.OriginalRequest, result.TargetURL, http.StatusFound)
		return true
	}
	return false
}

// SelectTarget 选择目标服务器
func (s *ProxyService) SelectTarget(req *ProxyRequest) (string, bool) {
	// 使用规则服务选择目标
	targetURL, isAltTarget := s.ruleService.GetTargetURL(s.client, req.OriginalRequest, req.PathConfig, req.TargetPath)
	return targetURL, isAltTarget
}

// CreateProxyRequest 创建代理请求
func (s *ProxyService) CreateProxyRequest(req *ProxyRequest, targetURL string) (*http.Request, error) {
	// 构建完整的目标URL
	fullTargetURL := s.buildTargetURL(targetURL, req.TargetPath, req.OriginalRequest.URL.RawQuery)
	
	// 创建新请求
	proxyReq, err := http.NewRequestWithContext(
		req.OriginalRequest.Context(),
		req.OriginalRequest.Method,
		fullTargetURL,
		req.OriginalRequest.Body,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %v", err)
	}

	// 复制头部
	s.copyHeaders(proxyReq.Header, req.OriginalRequest.Header)
	
	// 设置必要的头部
	if parsedURL, err := url.Parse(fullTargetURL); err == nil {
		proxyReq.Header.Set("Host", parsedURL.Host)
		proxyReq.Host = parsedURL.Host
		
		// 添加常见浏览器User-Agent
		if req.OriginalRequest.Header.Get("User-Agent") == "" {
			proxyReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		}

		// 设置Origin和Referer
		hostScheme := parsedURL.Scheme + "://" + parsedURL.Host
		proxyReq.Header.Set("Origin", hostScheme)
		proxyReq.Header.Set("Referer", hostScheme+"/")

		// 确保设置适当的Accept头
		if req.OriginalRequest.Header.Get("Accept") == "" {
			proxyReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		}

		// 处理Accept-Encoding
		if ae := req.OriginalRequest.Header.Get("Accept-Encoding"); ae != "" {
			proxyReq.Header.Set("Accept-Encoding", ae)
		} else {
			proxyReq.Header.Del("Accept-Encoding")
		}

		// 特别处理图片请求
		if utils.IsImageRequest(req.OriginalRequest.URL.Path) {
			accept := req.OriginalRequest.Header.Get("Accept")
			switch {
			case strings.Contains(accept, "image/avif"):
				proxyReq.Header.Set("Accept", "image/avif")
			case strings.Contains(accept, "image/webp"):
				proxyReq.Header.Set("Accept", "image/webp")
			}
			// 设置 Cloudflare 特定的头部
			proxyReq.Header.Set("CF-Image-Format", "auto")
		}

		// 设置代理头部
		clientIP := iputil.GetClientIP(req.OriginalRequest)
		proxyReq.Header.Set("X-Real-IP", clientIP)

		// 设置X-Forwarded-For
		if clientIP != "" {
			if prior := proxyReq.Header.Get("X-Forwarded-For"); prior != "" {
				proxyReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
			} else {
				proxyReq.Header.Set("X-Forwarded-For", clientIP)
			}
		}

		// 处理Cookie安全属性
		if req.OriginalRequest.TLS != nil && len(proxyReq.Cookies()) > 0 {
			cookies := proxyReq.Cookies()
			for _, cookie := range cookies {
				if !cookie.Secure {
					cookie.Secure = true
				}
				if !cookie.HttpOnly {
					cookie.HttpOnly = true
				}
			}
		}
	}

	return proxyReq, nil
}

// ExecuteRequest 执行代理请求
func (s *ProxyService) ExecuteRequest(proxyReq *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %v", err)
	}
	return resp, nil
}

// ProcessResponse 处理代理响应
func (s *ProxyService) ProcessResponse(req *ProxyRequest, resp *http.Response, w http.ResponseWriter, altTarget bool) (int64, error) {
	// 复制响应头
	s.copyHeaders(w.Header(), resp.Header)
	
	// 设置代理相关头部
	w.Header().Set("Proxy-Go-Cache-HIT", "0")
	if altTarget {
		w.Header().Set("Proxy-Go-AltTarget", "1")
	} else {
		w.Header().Set("Proxy-Go-AltTarget", "0")
	}
	
	// 对于图片请求，添加 Vary 头部以支持 CDN 基于 Accept 头部的缓存
	if utils.IsImageRequest(req.OriginalRequest.URL.Path) {
		// 添加 Vary: Accept 头部，让 CDN 知道响应会根据 Accept 头部变化
		if existingVary := w.Header().Get("Vary"); existingVary != "" {
			if !strings.Contains(existingVary, "Accept") {
				w.Header().Set("Vary", existingVary+", Accept")
			}
		} else {
			w.Header().Set("Vary", "Accept")
		}
	}
	
	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	var written int64
	var err error

	// 处理缓存写入
	if s.shouldCache(req, resp) {
		written, err = s.processWithCache(req, resp, w)
	} else {
		// 使用缓冲的复制提高性能
		bufSize := 32 * 1024 // 32KB 缓冲区
		buf := make([]byte, bufSize)
		written, err = io.CopyBuffer(w, resp.Body, buf)
	}

	if err != nil && !s.isConnectionClosed(err) {
		return written, fmt.Errorf("error writing response: %v", err)
	}

	return written, nil
}

// shouldCache 判断是否应该缓存
func (s *ProxyService) shouldCache(req *ProxyRequest, resp *http.Response) bool {
	return req.OriginalRequest.Method == http.MethodGet &&
		resp.StatusCode == http.StatusOK &&
		s.cache != nil
}

// processWithCache 处理带缓存的响应
func (s *ProxyService) processWithCache(req *ProxyRequest, resp *http.Response, w http.ResponseWriter) (int64, error) {
	cacheKey := s.cache.GenerateCacheKey(req.OriginalRequest)

	if cacheFile, err := s.cache.CreateTemp(cacheKey, resp); err == nil {
		// 使用缓冲IO提高性能
		bufSize := 32 * 1024 // 32KB 缓冲区
		buf := make([]byte, bufSize)

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
			// 如果关闭失败，删除临时文件
			if closeErr != nil {
				cacheFile.Close() // 尝试再次关闭
			}
		}

		return written, err
	}

	// 使用缓冲的复制提高性能
	bufSize := 32 * 1024 // 32KB 缓冲区
	buf := make([]byte, bufSize)
	return io.CopyBuffer(w, resp.Body, buf)
}

// buildTargetURL 构建目标URL
func (s *ProxyService) buildTargetURL(baseURL, targetPath, rawQuery string) string {
	// 解析基础URL
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		// 如果解析失败，回退到简单字符串拼接
		targetURL := baseURL + targetPath
		if rawQuery != "" {
			targetURL += "?" + rawQuery
		}
		return targetURL
	}
	
	// 正确处理路径，保持URL编码
	parsedBase.Path = strings.TrimSuffix(parsedBase.Path, "/") + targetPath
	if rawQuery != "" {
		parsedBase.RawQuery = rawQuery
	}
	
	return parsedBase.String()
}

// copyHeaders 复制HTTP头部，过滤hop-by-hop头部
func (s *ProxyService) copyHeaders(dst, src http.Header) {
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

	// 添加需要过滤的安全头部
	securityHeaders := map[string]bool{
		"Content-Security-Policy":             true,
		"Content-Security-Policy-Report-Only": true,
		"X-Content-Security-Policy":           true,
		"X-WebKit-CSP":                        true,
	}

	// 复制非 hop-by-hop 头部和安全头部
	for name, values := range src {
		if !hopHeaders[name] && !securityHeaders[name] {
			dst[name] = values
		}
	}
}

// isConnectionClosed 检查是否为连接关闭错误
func (s *ProxyService) isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "use of closed network connection")
}

// CreateLogEntry 创建访问日志条目
func (s *ProxyService) CreateLogEntry(req *ProxyRequest, statusCode int, duration time.Duration, bytesWritten int64, targetURL string) string {
	return fmt.Sprintf("[Proxy] %s %s -> %d (%s) %s from %s (target: %s)",
		req.OriginalRequest.Method, req.OriginalRequest.URL.Path, statusCode,
		iputil.GetClientIP(req.OriginalRequest), utils.FormatBytes(bytesWritten),
		utils.GetRequestSource(req.OriginalRequest), targetURL)
}