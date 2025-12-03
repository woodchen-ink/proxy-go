package sync

import (
	"fmt"
	"os"
)

// D1Config D1同步配置
type D1Config struct {
	Endpoint string // Worker URL
	Token    string // API Token
}

// NewD1ConfigFromEnv 从环境变量创建D1配置
func NewD1ConfigFromEnv() (*D1Config, error) {
	config := &D1Config{
		Endpoint: getEnvDefault("D1_SYNC_URL", ""),
		Token:    getEnvDefault("D1_SYNC_TOKEN", ""),
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid D1 config: %w", err)
	}

	return config, nil
}

// Validate 验证D1配置
func (c *D1Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("D1 endpoint URL is required (D1_SYNC_URL)")
	}
	if c.Token == "" {
		return fmt.Errorf("D1 API token is required (D1_SYNC_TOKEN)")
	}
	return nil
}

// IsD1ConfigComplete 检查D1同步配置是否完整
func IsD1ConfigComplete() bool {
	return os.Getenv("D1_SYNC_URL") != "" && os.Getenv("D1_SYNC_TOKEN") != ""
}

// IsEnabled 检查同步功能是否启用（基于环境变量）
// 为了兼容旧代码，保留此函数
func IsConfigComplete() bool {
	return IsD1ConfigComplete()
}

// getEnvDefault 获取环境变量，如果不存在则返回默认值
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
