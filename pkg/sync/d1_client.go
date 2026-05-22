package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// D1Client D1数据库客户端 (列式存储)
type D1Client struct {
	endpoint string
	token    string
	client   *http.Client
}

// NewD1Client 创建D1客户端
func NewD1Client(endpoint, token string) *D1Client {
	return &D1Client{
		endpoint: endpoint,
		token:    token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ============================================
// Path Stats
// ============================================

type PathStat struct {
	Path              string  `json:"path"`
	RequestCount      int64   `json:"request_count"`
	ErrorCount        int64   `json:"error_count"`
	BytesTransferred  int64   `json:"bytes_transferred"`
	Status2xx         int64   `json:"status_2xx"`
	Status3xx         int64   `json:"status_3xx"`
	Status4xx         int64   `json:"status_4xx"`
	Status5xx         int64   `json:"status_5xx"`
	CacheHits         int64   `json:"cache_hits"`
	CacheMisses       int64   `json:"cache_misses"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	BytesSaved        int64   `json:"bytes_saved"`
	AvgLatency        string  `json:"avg_latency,omitempty"`
	LastAccessTime    int64   `json:"last_access_time,omitempty"`
	UpdatedAt         int64   `json:"updated_at"`
}

func (c *D1Client) GetPathStats(ctx context.Context, path string) ([]PathStat, error) {
	url := fmt.Sprintf("%s/path-stats", c.endpoint)
	if path != "" {
		url += fmt.Sprintf("?path=%s", path)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool       `json:"success"`
		Data    []PathStat `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

func (c *D1Client) BatchUpsertPathStats(ctx context.Context, stats []PathStat) error {
	reqBody := map[string]any{"stats": stats}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/path-stats", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================
// Banned IPs
// ============================================

type BannedIP struct {
	IP           string `json:"ip"`
	BanTime      int64  `json:"ban_time"`
	BanEndTime   int64  `json:"ban_end_time"`
	Reason       string `json:"reason,omitempty"`
	ErrorCount   int    `json:"error_count"`
	IsActive     bool   `json:"is_active"`
	UnbanTime    int64  `json:"unban_time,omitempty"`
	UnbanReason  string `json:"unban_reason,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

type BannedIPHistory struct {
	ID          int64  `json:"id,omitempty"`
	IP          string `json:"ip"`
	BanTime     int64  `json:"ban_time"`
	BanEndTime  int64  `json:"ban_end_time"`
	Reason      string `json:"reason,omitempty"`
	ErrorCount  int    `json:"error_count"`
	UnbanTime   int64  `json:"unban_time,omitempty"`
	UnbanReason string `json:"unban_reason,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

func (c *D1Client) GetBannedIPs(ctx context.Context, activeOnly bool) ([]BannedIP, error) {
	url := fmt.Sprintf("%s/banned-ips", c.endpoint)
	if activeOnly {
		url += "?active=true"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool        `json:"success"`
		Data    []BannedIP  `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

func (c *D1Client) BatchUpsertBannedIPs(ctx context.Context, bans []BannedIP) error {
	reqBody := map[string]any{"bans": bans}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/banned-ips", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================
// Config Maps
// ============================================

type ConfigMap struct {
	Path           string `json:"path"`
	DefaultTarget  string `json:"default_target"`
	Enabled        int    `json:"enabled"` // D1 返回整数 0/1
	ExtensionRules string `json:"extension_rules,omitempty"` // JSON
	CacheConfig    string `json:"cache_config,omitempty"`    // JSON
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// IsEnabled 返回是否启用
func (c ConfigMap) IsEnabled() bool {
	return c.Enabled == 1
}

func (c *D1Client) GetConfigMaps(ctx context.Context, enabledOnly bool) ([]ConfigMap, error) {
	url := fmt.Sprintf("%s/config-maps", c.endpoint)
	if enabledOnly {
		url += "?enabled=true"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool        `json:"success"`
		Data    []ConfigMap `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

func (c *D1Client) BatchUpsertConfigMaps(ctx context.Context, maps []ConfigMap) error {
	reqBody := map[string]any{"maps": maps}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/config-maps", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteConfigMap 删除单个配置路径
func (c *D1Client) DeleteConfigMap(ctx context.Context, path string) error {
	url := fmt.Sprintf("%s/config-maps/%s", c.endpoint, path)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================
// Config Other
// ============================================

type ConfigOther struct {
	Key         string `json:"key"`
	Value       string `json:"value"` // JSON
	Description string `json:"description,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (c *D1Client) GetConfigOther(ctx context.Context, key string) ([]ConfigOther, error) {
	url := fmt.Sprintf("%s/config-other", c.endpoint)
	if key != "" {
		url += fmt.Sprintf("?key=%s", key)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool          `json:"success"`
		Data    []ConfigOther `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

func (c *D1Client) BatchUpsertConfigOther(ctx context.Context, configs []ConfigOther) error {
	reqBody := map[string]any{"configs": configs}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/config-other", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================
// Metrics (status_codes, latency_distribution)
// ============================================

type StatusCodeMetric struct {
	StatusCode string `json:"status_code"`
	Count      int64  `json:"count"`
	UpdatedAt  int64  `json:"updated_at"`
}

type LatencyMetric struct {
	Bucket    string `json:"bucket"`
	Count     int64  `json:"count"`
	UpdatedAt int64  `json:"updated_at"`
}

func (c *D1Client) GetStatusCodes(ctx context.Context) ([]StatusCodeMetric, error) {
	url := fmt.Sprintf("%s/metrics/status-codes", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool               `json:"success"`
		Data    []StatusCodeMetric `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

func (c *D1Client) BatchUpsertStatusCodes(ctx context.Context, metrics []StatusCodeMetric) error {
	if len(metrics) == 0 {
		return nil
	}

	reqBody := map[string]any{"metrics": metrics}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/metrics/status-codes", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *D1Client) GetLatencyDistribution(ctx context.Context) ([]LatencyMetric, error) {
	url := fmt.Sprintf("%s/metrics/latency", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool            `json:"success"`
		Data    []LatencyMetric `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// MigrationResult /migrations/apply 返回结构
type MigrationResult struct {
	Applied []string `json:"applied"`
	Skipped []string `json:"skipped"`
}

// ApplyMigrations 触发 worker 端自动迁移, 已应用的 id 自动跳过
//
// 设计为启动期一次性调用; 失败时返回 error, 调用方可记录日志后继续 (D1 不可用不应阻塞主服务启动)
func (c *D1Client) ApplyMigrations(ctx context.Context) (*MigrationResult, error) {
	url := fmt.Sprintf("%s/migrations/apply", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool     `json:"success"`
		Applied []string `json:"applied"`
		Skipped []string `json:"skipped"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &MigrationResult{Applied: result.Applied, Skipped: result.Skipped}, nil
}

// PathTimeseriesPoint 单节点单小时的桶记录 (上报)
type PathTimeseriesPoint struct {
	Path      string `json:"path"`
	TsHour    int64  `json:"ts_hour"`
	NodeID    string `json:"node_id,omitempty"`
	Requests  int64  `json:"requests"`
	Bytes     int64  `json:"bytes"`
	Errors    int64  `json:"errors"`
	UpdatedAt int64  `json:"updated_at"`
}

// AggregatedHourPoint 聚合后单小时记录 (跨节点求和)
type AggregatedHourPoint struct {
	Path     string `json:"path"`
	TsHour   int64  `json:"ts_hour"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
	Errors   int64  `json:"errors"`
}

// BatchUpsertPathTimeseries 上报本节点 (path, ts_hour, node_id) 的桶, idempotent 覆盖
func (c *D1Client) BatchUpsertPathTimeseries(ctx context.Context, points []PathTimeseriesPoint) error {
	if len(points) == 0 {
		return nil
	}
	reqBody := map[string]any{"points": points}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/metrics/path-timeseries", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetAggregatedTimeseries 拉取 [minHour, maxHour] 区间所有路径聚合后的小时序列
func (c *D1Client) GetAggregatedTimeseries(ctx context.Context, minHour, maxHour int64) ([]AggregatedHourPoint, error) {
	url := fmt.Sprintf("%s/metrics/path-timeseries?min_hour=%d&max_hour=%d", c.endpoint, minHour, maxHour)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool                  `json:"success"`
		Data    []AggregatedHourPoint `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Data, nil
}

// PruneTimeseries 删除 ts_hour < cutoffHour 的旧桶
func (c *D1Client) PruneTimeseries(ctx context.Context, cutoffHour int64) (int, error) {
	url := fmt.Sprintf("%s/metrics/path-timeseries?cutoff_hour=%d", c.endpoint, cutoffHour)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
		Deleted int  `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Deleted, nil
}

// RefererDailyPoint 单节点单天单 host 的桶记录 (上报)
type RefererDailyPoint struct {
	Host      string `json:"host"`
	TsDate    int64  `json:"ts_date"`
	NodeID    string `json:"node_id,omitempty"`
	Requests  int64  `json:"requests"`
	Bytes     int64  `json:"bytes"`
	Errors    int64  `json:"errors"`
	UpdatedAt int64  `json:"updated_at"`
}

// AggregatedRefererDayPoint 聚合后单天单 host 记录 (跨节点求和)
type AggregatedRefererDayPoint struct {
	Host     string `json:"host"`
	TsDate   int64  `json:"ts_date"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
	Errors   int64  `json:"errors"`
}

// BatchUpsertRefererDaily 上报本节点天级 referer host 桶
func (c *D1Client) BatchUpsertRefererDaily(ctx context.Context, points []RefererDailyPoint) error {
	if len(points) == 0 {
		return nil
	}
	reqBody := map[string]any{"points": points}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/metrics/referer-daily", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetAggregatedRefererDaily 拉取 [minDate, maxDate] 区间内所有 host 聚合后的天序列
func (c *D1Client) GetAggregatedRefererDaily(ctx context.Context, minDate, maxDate int64) ([]AggregatedRefererDayPoint, error) {
	url := fmt.Sprintf("%s/metrics/referer-daily?min_date=%d&max_date=%d", c.endpoint, minDate, maxDate)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool                        `json:"success"`
		Data    []AggregatedRefererDayPoint `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Data, nil
}

// PruneRefererDaily 删除 ts_date < cutoffDate 的旧桶
func (c *D1Client) PruneRefererDaily(ctx context.Context, cutoffDate int64) (int, error) {
	url := fmt.Sprintf("%s/metrics/referer-daily?cutoff_date=%d", c.endpoint, cutoffDate)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
		Deleted int  `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Deleted, nil
}

func (c *D1Client) BatchUpsertLatencyDistribution(ctx context.Context, metrics []LatencyMetric) error {
	if len(metrics) == 0 {
		return nil
	}

	reqBody := map[string]any{"metrics": metrics}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/metrics/latency", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
