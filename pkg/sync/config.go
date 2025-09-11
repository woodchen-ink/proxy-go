package sync

import (
	"fmt"
	"os"
)

// NewConfigFromEnv 从环境变量创建配置
func NewConfigFromEnv() (*Config, error) {
	config := &Config{
		Endpoint:        getEnvDefault("SYNC_S3_ENDPOINT", ""),
		Bucket:          getEnvDefault("SYNC_S3_BUCKET", ""),
		Region:          getEnvDefault("SYNC_S3_REGION", "us-east-1"),
		AccessKeyID:     getEnvDefault("SYNC_S3_ACCESS_KEY_ID", ""),
		SecretAccessKey: getEnvDefault("SYNC_S3_SECRET_ACCESS_KEY", ""),
		UsePathStyle:    getEnvBool("SYNC_S3_USE_PATH_STYLE", false),
		ConfigPath:      getEnvDefault("SYNC_CONFIG_PATH", "data/proxy-go"),
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sync config: %w", err)
	}

	return config, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket name is required")
	}

	if c.AccessKeyID == "" {
		return fmt.Errorf("access key ID is required")
	}

	if c.SecretAccessKey == "" {
		return fmt.Errorf("secret access key is required")
	}

	if c.Region == "" {
		return fmt.Errorf("region is required")
	}

	if c.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}

	return nil
}

// getEnvDefault 获取环境变量，如果不存在则返回默认值
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool 获取布尔型环境变量
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}

// IsConfigComplete 检查同步配置是否完整（基于环境变量）
func IsConfigComplete() bool {
	// 检查必需的环境变量是否存在
	requiredEnvs := []string{
		"SYNC_S3_BUCKET",
		"SYNC_S3_ACCESS_KEY_ID", 
		"SYNC_S3_SECRET_ACCESS_KEY",
		"SYNC_S3_REGION",
	}
	
	for _, env := range requiredEnvs {
		if os.Getenv(env) == "" {
			return false
		}
	}
	
	return true
}
