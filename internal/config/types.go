package config

type Config struct {
	MAP         map[string]string `json:"MAP"`
	Compression CompressionConfig `json:"Compression"`
	FixedPaths  []FixedPathConfig `json:"FixedPaths"` // 新增
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

type FixedPathConfig struct {
	Path       string `json:"Path"`
	TargetHost string `json:"TargetHost"`
	TargetURL  string `json:"TargetURL"`
}
