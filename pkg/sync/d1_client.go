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

// D1Client D1数据库客户端
type D1Client struct {
	endpoint string
	token    string
	client   *http.Client
}

// D1Response D1 API响应
type D1Response struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	UpdatedAt int64           `json:"updated_at"`
}

// D1SaveRequest D1保存请求
type D1SaveRequest struct {
	Data any `json:"data"`
}

// D1SaveResponse D1保存响应
type D1SaveResponse struct {
	Success   bool   `json:"success"`
	Type      string `json:"type"`
	UpdatedAt int64  `json:"updated_at"`
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

// Upload 上传数据到D1
func (c *D1Client) Upload(ctx context.Context, path string, data []byte) error {
	// 路径格式: "data/config.json" -> type: "config"
	dataType := c.extractType(path)
	if dataType == "" {
		return fmt.Errorf("invalid path format: %s", path)
	}

	// 解析数据为JSON
	var jsonData any
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}

	// 构建请求
	reqBody := D1SaveRequest{Data: jsonData}
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送POST请求
	url := fmt.Sprintf("%s/%s", c.endpoint, dataType)
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

	var saveResp D1SaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&saveResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !saveResp.Success {
		return fmt.Errorf("D1 save failed for type: %s", dataType)
	}

	return nil
}

// Download 从D1下载数据
func (c *D1Client) Download(ctx context.Context, path string) ([]byte, error) {
	// 路径格式: "data/config.json" -> type: "config"
	dataType := c.extractType(path)
	if dataType == "" {
		return nil, fmt.Errorf("invalid path format: %s", path)
	}

	// 发送GET请求
	url := fmt.Sprintf("%s/%s", c.endpoint, dataType)
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("data not found in D1")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var d1Resp D1Response
	if err := json.NewDecoder(resp.Body).Decode(&d1Resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 返回原始数据
	return d1Resp.Data, nil
}

// GetVersion 获取数据版本
func (c *D1Client) GetVersion(ctx context.Context, path string) (string, time.Time, error) {
	// 路径格式: "data/config.json" -> type: "config"
	dataType := c.extractType(path)
	if dataType == "" {
		return "", time.Time{}, fmt.Errorf("invalid path format: %s", path)
	}

	// 发送GET请求
	url := fmt.Sprintf("%s/%s", c.endpoint, dataType)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to create request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", time.Time{}, nil // 不存在返回空版本
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("D1 API error (status %d): %s", resp.StatusCode, string(body))
	}

	var d1Resp D1Response
	if err := json.NewDecoder(resp.Body).Decode(&d1Resp); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to decode response: %w", err)
	}

	// 将时间戳转换为版本字符串和时间
	version := fmt.Sprintf("%d", d1Resp.UpdatedAt)
	timestamp := time.UnixMilli(d1Resp.UpdatedAt)

	return version, timestamp, nil
}

// extractType 从路径提取数据类型
// "data/config.json" -> "config"
// "data/path_stats.json" -> "path_stats"
// "data/banned_ips.json" -> "banned_ips"
func (c *D1Client) extractType(path string) string {
	// 支持的数据类型映射
	typeMap := map[string]string{
		"data/config.json":      "config",
		"data/path_stats.json":  "path_stats",
		"data/banned_ips.json":  "banned_ips",
		// 兼容旧的路径格式
		"/config.json":          "config",
		"/path_stats.json":      "path_stats",
		"/banned_ips.json":      "banned_ips",
		"config.json":           "config",
		"path_stats.json":       "path_stats",
		"banned_ips.json":       "banned_ips",
	}

	if dataType, ok := typeMap[path]; ok {
		return dataType
	}

	return ""
}
