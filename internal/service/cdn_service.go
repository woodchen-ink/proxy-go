package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"proxy-go/internal/config"
	"sort"
	"strings"
	"time"
)

// CDN purge 通用错误
var (
	ErrCDNNoEnabledProvider = errors.New("no enabled CDN provider")
	ErrCDNProviderNotFound  = errors.New("CDN provider not found")
	ErrCDNUnsupportedType   = errors.New("unsupported CDN provider type")
	ErrCDNInvalidPurgeType  = errors.New("invalid purge type")
	ErrCDNMissingTarget     = errors.New("purge target is empty")
)

// CDN purge 类型, 跨厂商统一约定
const (
	CDNPurgeTypeAll      = "all"
	CDNPurgeTypeURLs     = "urls"
	CDNPurgeTypePrefixes = "prefixes"
	CDNPurgeTypeHosts    = "hosts"
	CDNPurgeTypeTags     = "tags"
)

// 支持的 provider 类型
const (
	CDNProviderCloudflare = "cloudflare"
	CDNProviderEdgeOne    = "edgeone"
)

// CDNPurgeRequest 业务层 purge 请求
type CDNPurgeRequest struct {
	Type    string   `json:"type"`
	Targets []string `json:"targets"`
}

// CDNPurgeResult 业务层 purge 响应
// Raw 透传厂商原始返回, 便于前端展示故障详情
type CDNPurgeResult struct {
	Success    bool   `json:"success"`
	ProviderID string `json:"provider_id"`
	Provider   string `json:"provider"`
	JobID      string `json:"job_id,omitempty"`
	Message    string `json:"message,omitempty"`
	Raw        any    `json:"raw,omitempty"`
}

// CDNService 负责 CDN provider 配置 CRUD 与 purge 调度
type CDNService struct {
	configManager *config.ConfigManager
	httpClient    *http.Client
	now           func() time.Time
}

// NewCDNService 构造 CDNService, httpClient 走 30s 超时, 失败由调用方决定是否重试
func NewCDNService(cm *config.ConfigManager) *CDNService {
	return &CDNService{
		configManager: cm,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		now:           time.Now,
	}
}

// ListProviders 列出所有 provider 配置 (含凭据), 仅供已认证管理员调用
func (s *CDNService) ListProviders() []config.CDNProvider {
	cfg := s.configManager.GetConfig()
	if cfg == nil || cfg.CDN.Providers == nil {
		return []config.CDNProvider{}
	}
	out := make([]config.CDNProvider, len(cfg.CDN.Providers))
	copy(out, cfg.CDN.Providers)
	return out
}

// SaveProviders 整体覆盖 provider 列表, 由 UI 一次性提交
// 单一启用约束: 最多一个 Enabled=true, 多于一个直接拒绝
func (s *CDNService) SaveProviders(providers []config.CDNProvider) error {
	enabledCount := 0
	seenIDs := make(map[string]bool, len(providers))
	for i := range providers {
		p := &providers[i]
		if err := validateCDNProvider(p); err != nil {
			return err
		}
		if seenIDs[p.ID] {
			return fmt.Errorf("provider ID 重复: %s", p.ID)
		}
		seenIDs[p.ID] = true
		if p.Enabled {
			enabledCount++
		}
	}
	if enabledCount > 1 {
		return errors.New("仅允许启用 1 个 CDN provider")
	}

	cfg := s.configManager.GetConfig()
	if cfg == nil {
		return errors.New("配置未初始化")
	}
	newCfg := *cfg
	newCfg.CDN = config.CDNConfig{Providers: providers}
	return s.configManager.UpdateConfig(&newCfg)
}

// Purge 调度启用中的 provider 执行 purge
func (s *CDNService) Purge(ctx context.Context, req CDNPurgeRequest) (*CDNPurgeResult, error) {
	provider, err := s.activeProvider()
	if err != nil {
		return nil, err
	}
	return s.PurgeWithProvider(ctx, provider, req)
}

// PurgeWithProvider 按指定 provider 执行 purge (供测试或显式调用)
func (s *CDNService) PurgeWithProvider(ctx context.Context, provider config.CDNProvider, req CDNPurgeRequest) (*CDNPurgeResult, error) {
	if err := validateCDNPurgeRequest(req); err != nil {
		return nil, err
	}

	switch provider.Type {
	case CDNProviderCloudflare:
		return s.purgeCloudflare(ctx, provider, req)
	case CDNProviderEdgeOne:
		return s.purgeEdgeOne(ctx, provider, req)
	default:
		return nil, fmt.Errorf("%w: %s", ErrCDNUnsupportedType, provider.Type)
	}
}

// activeProvider 取当前启用的 provider, 没有则返回错误
func (s *CDNService) activeProvider() (config.CDNProvider, error) {
	cfg := s.configManager.GetConfig()
	if cfg == nil {
		return config.CDNProvider{}, errors.New("配置未初始化")
	}
	for _, p := range cfg.CDN.Providers {
		if p.Enabled {
			return p, nil
		}
	}
	return config.CDNProvider{}, ErrCDNNoEnabledProvider
}

// validateCDNProvider 校验单个 provider 的必填项
func validateCDNProvider(p *config.CDNProvider) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("provider ID 不能为空")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider 名称不能为空")
	}
	if p.Credentials == nil {
		p.Credentials = map[string]string{}
	}
	switch p.Type {
	case CDNProviderCloudflare:
		if strings.TrimSpace(p.Credentials["apiToken"]) == "" {
			return errors.New("Cloudflare provider 缺少 apiToken")
		}
		if strings.TrimSpace(p.Credentials["zoneId"]) == "" {
			return errors.New("Cloudflare provider 缺少 zoneId")
		}
	case CDNProviderEdgeOne:
		if strings.TrimSpace(p.Credentials["secretId"]) == "" {
			return errors.New("EdgeOne provider 缺少 secretId")
		}
		if strings.TrimSpace(p.Credentials["secretKey"]) == "" {
			return errors.New("EdgeOne provider 缺少 secretKey")
		}
		if strings.TrimSpace(p.Credentials["zoneId"]) == "" {
			return errors.New("EdgeOne provider 缺少 zoneId")
		}
	default:
		return fmt.Errorf("%w: %s", ErrCDNUnsupportedType, p.Type)
	}
	return nil
}

// validateCDNPurgeRequest 检查 purge 请求合法性
func validateCDNPurgeRequest(req CDNPurgeRequest) error {
	switch req.Type {
	case CDNPurgeTypeAll:
		return nil
	case CDNPurgeTypeURLs, CDNPurgeTypePrefixes, CDNPurgeTypeHosts, CDNPurgeTypeTags:
		if len(req.Targets) == 0 {
			return ErrCDNMissingTarget
		}
		for _, t := range req.Targets {
			if strings.TrimSpace(t) == "" {
				return ErrCDNMissingTarget
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrCDNInvalidPurgeType, req.Type)
	}
}

// ---- Cloudflare ----

// purgeCloudflare 调用 Cloudflare REST API 清理缓存
// 参考: https://developers.cloudflare.com/api/operations/zone-purge
func (s *CDNService) purgeCloudflare(ctx context.Context, provider config.CDNProvider, req CDNPurgeRequest) (*CDNPurgeResult, error) {
	apiToken := provider.Credentials["apiToken"]
	zoneID := provider.Credentials["zoneId"]
	endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/purge_cache", zoneID)

	body := map[string]any{}
	switch req.Type {
	case CDNPurgeTypeAll:
		body["purge_everything"] = true
	case CDNPurgeTypeURLs:
		body["files"] = req.Targets
	case CDNPurgeTypeTags:
		body["tags"] = req.Targets
	case CDNPurgeTypeHosts:
		body["hosts"] = req.Targets
	case CDNPurgeTypePrefixes:
		body["prefixes"] = req.Targets
	}

	raw, err := s.doJSON(ctx, http.MethodPost, endpoint, map[string]string{
		"Authorization": "Bearer " + apiToken,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}

	parsed := struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}{}
	_ = json.Unmarshal(raw, &parsed)

	result := &CDNPurgeResult{
		ProviderID: provider.ID,
		Provider:   provider.Type,
		Success:    parsed.Success,
		JobID:      parsed.Result.ID,
		Raw:        json.RawMessage(raw),
	}
	if !parsed.Success {
		if len(parsed.Errors) > 0 {
			result.Message = parsed.Errors[0].Message
		} else {
			result.Message = "Cloudflare purge 失败"
		}
		return result, fmt.Errorf("cloudflare purge: %s", result.Message)
	}
	return result, nil
}

// ---- Tencent EdgeOne (TEO) ----

// purgeEdgeOne 调用腾讯云 EdgeOne CreatePurgeTask, 使用 TC3-HMAC-SHA256 签名
// 参考: https://cloud.tencent.com/document/product/1552/80721
func (s *CDNService) purgeEdgeOne(ctx context.Context, provider config.CDNProvider, req CDNPurgeRequest) (*CDNPurgeResult, error) {
	secretID := provider.Credentials["secretId"]
	secretKey := provider.Credentials["secretKey"]
	zoneID := provider.Credentials["zoneId"]

	eoType := mapPurgeTypeToEdgeOne(req.Type)
	if eoType == "" {
		return nil, fmt.Errorf("%w: edgeone 不支持 %s", ErrCDNInvalidPurgeType, req.Type)
	}

	payload := map[string]any{
		"ZoneId":  zoneID,
		"Type":    eoType,
		"Targets": req.Targets,
	}
	if req.Type == CDNPurgeTypeAll {
		payload["Targets"] = []string{}
	}

	const (
		host    = "teo.tencentcloudapi.com"
		service = "teo"
		action  = "CreatePurgeTask"
		version = "2022-09-01"
		region  = "ap-guangzhou"
	)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 EdgeOne 请求失败: %w", err)
	}

	headers := signTencentCloudV3(secretID, secretKey, service, host, payloadBytes, s.now())

	endpoint := "https://" + host
	respHeaders := map[string]string{
		"Host":           host,
		"Content-Type":   "application/json",
		"X-TC-Action":    action,
		"X-TC-Version":   version,
		"X-TC-Region":    region,
		"X-TC-Timestamp": headers["X-TC-Timestamp"],
		"Authorization":  headers["Authorization"],
	}

	raw, err := s.doRaw(ctx, http.MethodPost, endpoint, respHeaders, payloadBytes)
	if err != nil {
		return nil, err
	}

	parsed := struct {
		Response struct {
			RequestId  string   `json:"RequestId"`
			JobId      string   `json:"JobId"`
			FailedList []string `json:"FailedList"`
			Error      *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}{}
	_ = json.Unmarshal(raw, &parsed)

	result := &CDNPurgeResult{
		ProviderID: provider.ID,
		Provider:   provider.Type,
		JobID:      parsed.Response.JobId,
		Raw:        json.RawMessage(raw),
	}
	if parsed.Response.Error != nil {
		result.Success = false
		result.Message = parsed.Response.Error.Message
		return result, fmt.Errorf("edgeone purge: %s", result.Message)
	}
	result.Success = true
	return result, nil
}

// mapPurgeTypeToEdgeOne 把统一 purge 类型映射到 EdgeOne 的 Type 字段
func mapPurgeTypeToEdgeOne(t string) string {
	switch t {
	case CDNPurgeTypeAll:
		return "purge_all"
	case CDNPurgeTypeURLs:
		return "purge_url"
	case CDNPurgeTypePrefixes:
		return "purge_prefix"
	case CDNPurgeTypeHosts:
		return "purge_host"
	case CDNPurgeTypeTags:
		return "purge_cache_tag"
	default:
		return ""
	}
}

// signTencentCloudV3 生成腾讯云 TC3-HMAC-SHA256 签名头
// 返回包含 Authorization / X-TC-Timestamp 的最小头集合, 调用方再合入其他业务头
func signTencentCloudV3(secretID, secretKey, service, host string, payload []byte, now time.Time) map[string]string {
	timestamp := now.Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	signedHeaderMap := map[string]string{
		"content-type": "application/json",
		"host":         host,
	}
	keys := make([]string, 0, len(signedHeaderMap))
	for k := range signedHeaderMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalHeaders strings.Builder
	for _, k := range keys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.ToLower(signedHeaderMap[k]))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(keys, ";")

	hashedPayload := sha256Hex(payload)
	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders.String(),
		signedHeaders,
		hashedPayload,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	authorization := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, credentialScope, signedHeaders, signature,
	)
	return map[string]string{
		"Authorization":  authorization,
		"X-TC-Timestamp": fmt.Sprintf("%d", timestamp),
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

// ---- HTTP helpers ----

// doJSON 发起 JSON POST/PUT 请求并返回原始响应体
func (s *CDNService) doJSON(ctx context.Context, method, url string, headers map[string]string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	return s.doRaw(ctx, method, url, headers, payload)
}

// doRaw 发起带原始 body 的请求, 用于已自行序列化与签名的场景
func (s *CDNService) doRaw(ctx context.Context, method, url string, headers map[string]string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 %s 失败: %w", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode >= 500 {
		return raw, fmt.Errorf("上游返回 %d", resp.StatusCode)
	}
	return raw, nil
}
