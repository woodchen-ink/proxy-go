package compression

import (
	"io"

	"github.com/andybalholm/brotli"
)

type BrotliCompressor struct {
	level int
}

func NewBrotliCompressor(level int) *BrotliCompressor {
	// 确保level在有效范围内 (0-11)
	if level < 0 || level > 11 {
		level = brotli.DefaultCompression
	}
	return &BrotliCompressor{level: level}
}

func (b *BrotliCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return brotli.NewWriterLevel(w, b.level), nil
}
