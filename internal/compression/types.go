package compression

import "io"

// Compressor 定义压缩器接口
type Compressor interface {
	Compress(w io.Writer) (io.WriteCloser, error)
}

// CompressionType 表示压缩类型
type CompressionType string

const (
	CompressionGzip   CompressionType = "gzip"
	CompressionBrotli CompressionType = "br"
)

// Config 压缩配置结构体
type Config struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

// CompressorConfig 单个压缩器的配置
type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

// Manager 压缩管理器接口
type Manager interface {
	// SelectCompressor 根据 Accept-Encoding 头选择合适的压缩器
	// 返回选中的压缩器和对应的压缩类型
	SelectCompressor(acceptEncoding string) (Compressor, CompressionType)
}
