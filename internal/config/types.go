package config

type Config struct {
	MAP         map[string]string `json:"MAP"`
	Compression CompressionConfig `json:"Compression"`
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}
