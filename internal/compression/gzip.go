package compression

import (
	"compress/gzip"
	"io"
)

type GzipCompressor struct {
	level int
}

func NewGzipCompressor(level int) *GzipCompressor {
	// 确保level在有效范围内
	if level < gzip.DefaultCompression || level > gzip.BestCompression {
		level = gzip.DefaultCompression
	}
	return &GzipCompressor{level: level}
}

func (g *GzipCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return gzip.NewWriterLevel(w, g.level)
}
