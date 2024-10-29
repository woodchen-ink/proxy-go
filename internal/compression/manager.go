package compression

import "strings"

type compressionManager struct {
	gzip   Compressor
	brotli Compressor
	config Config
}

// NewManager 创建新的压缩管理器
func NewManager(config Config) Manager {
	m := &compressionManager{
		config: config,
	}

	if config.Gzip.Enabled {
		m.gzip = NewGzipCompressor(config.Gzip.Level)
	}

	if config.Brotli.Enabled {
		m.brotli = NewBrotliCompressor(config.Brotli.Level)
	}

	return m
}

// SelectCompressor 实现 Manager 接口
func (m *compressionManager) SelectCompressor(acceptEncoding string) (Compressor, CompressionType) {
	// 优先选择 brotli
	if m.brotli != nil && strings.Contains(acceptEncoding, string(CompressionBrotli)) {
		return m.brotli, CompressionBrotli
	}

	// 其次选择 gzip
	if m.gzip != nil && strings.Contains(acceptEncoding, string(CompressionGzip)) {
		return m.gzip, CompressionGzip
	}

	return nil, ""
}
