package service

import (
	"fmt"
	"io"
	"log"
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

	// cacheKey 是延迟生成的缓存键，每次请求最多生成一次
	cacheKey    cache.CacheKey
	cacheKeySet bool
}

// ProxyResponse 代理响应结构
type ProxyResponse struct {
	StatusCode    int
	Headers       http.Header
	Body          io.ReadCloser
	ContentLength int64
	FromCache     bool
	AltTarget     bool
	CacheKey      string
}

// ProxyResult 代理处理结果
type ProxyResult struct {
	Success      bool
	StatusCode   int
	ErrorMessage string
	BytesWritten int64
	Duration     time.Duration
	FromCache    bool
	AltTarget    bool
	TargetURL    string
}

type ProxyService struct {
	client          *http.Client
	cache           *cache.CacheManager
	ruleService     *RuleService
	redirectService *RedirectService
	retryConfig     RetryConfig // 重试配置
}

func NewProxyService(client *http.Client, cache *cache.CacheManager, ruleService *RuleService) *ProxyService {
	redirectService := NewRedirectService(ruleService)

	return &ProxyService{
		client:          client,
		cache:           cache,
		ruleService:     ruleService,
		redirectService: redirectService,
		retryConfig:     DefaultRetryConfig, // 使用默认重试配置
	}
}

// CheckCache 检查缓存
func (s *ProxyService) CheckCache(req *ProxyRequest) (*cache.CacheItem, bool, bool) {
	if req.OriginalRequest.Method != http.MethodGet || s.cache == nil {
		return nil, false, false
	}

	cacheKey := s.getOrBuildCacheKey(req)
	item, hit, notModified := s.cache.Get(cacheKey, req.OriginalRequest, req.PathConfig.CFImageOpt)
	return item, hit, notModified
}

// getOrBuildCacheKey 从 ProxyRequest 取缓存键，首次调用时生成并复用
func (s *ProxyService) getOrBuildCacheKey(req *ProxyRequest) cache.CacheKey {
	if !req.cacheKeySet {
		req.cacheKey = s.cache.GenerateCacheKey(req.OriginalRequest, req.PathConfig.CFImageOpt)
		req.cacheKeySet = true
	}
	return req.cacheKey
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

// BuildRefererRedirectURL 为路径级 Referer 重定向拼出目标 URL
// targetPrefix 为目标前缀 (如 https://cdn2.com), targetPath 为剥掉路径前缀后的子路径 (带前导 /),
// 结果换 host 去前缀并保留原 query。URL 拼接复用 RedirectService.buildTargetURL, 与扩展名规则跳转一致。
func (s *ProxyService) BuildRefererRedirectURL(targetPrefix, targetPath, rawQuery string) string {
	return s.redirectService.buildTargetURL(targetPrefix, targetPath, rawQuery)
}

// SelectTarget 选择目标服务器
func (s *ProxyService) SelectTarget(req *ProxyRequest) (string, bool) {
	// 使用规则服务选择目标
	targetURL, isAltTarget := s.ruleService.GetTargetURL(s.client, req.OriginalRequest, req.PathConfig, req.TargetPath)
	return targetURL, isAltTarget
}

// SelectTargets 返回有序的回源列表与是否命中扩展名规则。
//
// 优先级与 SelectTarget 一致: 扩展名规则命中时返回该规则的单个目标 (本功能不对扩展名规则做多源);
// 未命中扩展名规则时返回路径级多源列表 (PathConfig.GetTargets()), 供 ExecuteRequestWithFailover 按序回落。
func (s *ProxyService) SelectTargets(req *ProxyRequest) ([]string, bool) {
	targetURL, isAltTarget := s.ruleService.GetTargetURL(s.client, req.OriginalRequest, req.PathConfig, req.TargetPath)
	if isAltTarget {
		// 命中扩展名规则: 单源, 不参与多源回落
		return []string{targetURL}, true
	}
	// 未命中规则: 使用路径级有序多源列表
	targets := req.PathConfig.GetTargets()
	if len(targets) == 0 {
		// 兜底: GetTargets 为空时回落到 GetTargetURL 给的结果 (理论上不会发生)
		return []string{targetURL}, false
	}
	return targets, false
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

// ExecuteRequest 执行代理请求（带重试机制）
func (s *ProxyService) ExecuteRequest(proxyReq *http.Request) (*http.Response, error) {
	// 使用带重试的请求执行
	resp, err := ExecuteWithRetry(s.client, proxyReq, s.retryConfig)

	if err != nil {
		return nil, fmt.Errorf("proxy request failed after retries: %v", err)
	}
	return resp, nil
}

// canFailover 判断请求是否可以安全地跨多个回源重试。
//
// 两个条件同时满足才允许换源:
//  1. 方法幂等 (GET/HEAD/OPTIONS): 非幂等方法 (POST/PUT/PATCH/DELETE) 换源会放大副作用 (重复下单 / 重复写入), 一律只打首源
//  2. 无请求体 (ContentLength <= 0): CreateProxyRequest 直接复用 OriginalRequest.Body, 读一次即耗尽, 无法重放给下一个源
//
// 注: 服务端的 r.Body 永不为 nil (空体也是 http.NoBody), 故用 ContentLength 判断有无 body, 不靠 Body 身份。
func canFailover(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return r.ContentLength <= 0
	default:
		return false
	}
}

// ExecuteRequestWithFailover 按顺序对多个回源执行请求, 失败自动回落到下一个源。
//
// 触发回落: 连接级失败 (ExecuteWithRetry 同源重试耗尽后仍报错) 或 isFailoverStatusCode 命中的状态码。
// 返回最终采用的响应、命中的目标 URL、是否实际发生过回落。
//
// 边界处理:
//   - targets 为空: 返回 error
//   - 单源 / 请求不可回落 (带 body 的非幂等请求): 只打首源, 等价于旧行为
//   - 全部源都是可回落状态码: 返回最后一个响应 (让客户端拿到真实错误, 而不是吞掉)
//   - 全部源都是连接错误: 返回最后一个 error
func (s *ProxyService) ExecuteRequestWithFailover(req *ProxyRequest, targets []string) (*http.Response, string, bool, error) {
	if len(targets) == 0 {
		return nil, "", false, fmt.Errorf("no upstream target configured")
	}

	// 不可回落或单源: 只打首源, 走原有单源路径
	if len(targets) == 1 || !canFailover(req.OriginalRequest) {
		target := targets[0]
		httpReq, err := s.CreateProxyRequest(req, target)
		if err != nil {
			return nil, target, false, err
		}
		resp, err := s.ExecuteRequest(httpReq)
		return resp, target, false, err
	}

	// 最后一个源即使返回可回落状态码 (404/5xx) 也透传其真实响应, 不再换源 —— 见下方 i==末位 的判断。
	// 因此循环只会经由"构建请求失败 / 连接错误"两种 continue 走到循环外, lastErr 记录最后一次失败原因。
	var lastErr error
	for i, target := range targets {
		isLast := i == len(targets)-1

		httpReq, err := s.CreateProxyRequest(req, target)
		if err != nil {
			lastErr = err
			log.Printf("[Failover] 源 %d/%d (%s) 构建请求失败: %v", i+1, len(targets), target, err)
			continue
		}

		resp, err := s.ExecuteRequest(httpReq)
		if err != nil {
			// 同源重试已耗尽仍失败 (连接级错误), 换下一个源
			lastErr = err
			log.Printf("[Failover] 源 %d/%d (%s) 连接失败, 尝试下一个源: %v", i+1, len(targets), target, err)
			continue
		}

		// 非末位源命中可回落状态码: 关闭响应体, 尝试下一个源;
		// 末位源直接透传 (让客户端拿到真实错误, 而不是吞掉)
		if isFailoverStatusCode(resp.StatusCode) && !isLast {
			log.Printf("[Failover] 源 %d/%d (%s) 返回 %d, 尝试下一个源", i+1, len(targets), target, resp.StatusCode)
			resp.Body.Close()
			lastErr = fmt.Errorf("failover status code: %d from %s", resp.StatusCode, target)
			continue
		}

		// 命中可用响应 (或已是最后一个源, 透传其真实响应)
		didFailover := i > 0
		if didFailover {
			log.Printf("[Failover] 回落成功: 第 %d 个源 (%s) 返回 %d", i+1, target, resp.StatusCode)
		}
		return resp, target, didFailover, nil
	}

	// 所有源都因构建请求失败 / 连接错误而无响应
	return nil, targets[len(targets)-1], true, fmt.Errorf("all %d upstreams failed, last error: %v", len(targets), lastErr)
}

// ProcessResponse 处理代理响应
func (s *ProxyService) ProcessResponse(req *ProxyRequest, resp *http.Response, w http.ResponseWriter, altTarget bool) (int64, error) {
	// 复制响应头
	s.copyHeaders(w.Header(), resp.Header)

	// 设置代理相关头部
	w.Header().Set("CZL-Proxy-Cache-HIT", "0")
	if altTarget {
		w.Header().Set("CZL-Proxy-AltTarget", "1")
	} else {
		w.Header().Set("CZL-Proxy-AltTarget", "0")
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

// shouldCache 判断是否应该缓存
func (s *ProxyService) shouldCache(req *ProxyRequest, resp *http.Response) bool {
	return req.OriginalRequest.Method == http.MethodGet &&
		resp.StatusCode == http.StatusOK &&
		s.cache != nil
}

// processWithCache 处理带缓存的响应
func (s *ProxyService) processWithCache(req *ProxyRequest, resp *http.Response, w http.ResponseWriter) (int64, error) {
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
			// 如果关闭失败，删除临时文件
			if closeErr != nil {
				cacheFile.Close() // 尝试再次关闭
			}
		}

		return written, err
	}

	// 🚀 零拷贝优化: 使用 buffer pool 复用缓冲区
	buf := cache.GetBuffer(32 * 1024)
	defer cache.PutBuffer(buf)
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

// copyHeaders 复制HTTP头部，过滤hop-by-hop头部和敏感安全头部
func (s *ProxyService) copyHeaders(dst, src http.Header) {
	copyFilteredHeaders(dst, src, securityHeadersToStrip)
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
