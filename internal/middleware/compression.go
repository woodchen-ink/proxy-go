package middleware

import (
	"bufio"
	"io"
	"mime"
	"net"
	"net/http"
	"proxy-go/internal/compression"
	"strings"
)

const (
	defaultBufferSize = 32 * 1024 // 32KB
)

type CompressResponseWriter struct {
	http.ResponseWriter
	compressor     compression.Compressor
	writer         io.WriteCloser
	bufferedWriter *bufio.Writer
	statusCode     int
	written        bool
	compressed     bool
}

func CompressionMiddleware(manager compression.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查源站是否已经压缩
			if r.Header.Get("Content-Encoding") != "" {
				next.ServeHTTP(w, r)
				return
			}

			// 选择压缩器
			compressor, encoding := manager.SelectCompressor(r.Header.Get("Accept-Encoding"))
			if compressor == nil {
				next.ServeHTTP(w, r)
				return
			}

			cw := &CompressResponseWriter{
				ResponseWriter: w,
				compressor:     compressor,
				statusCode:     0,
				written:        false,
				compressed:     false,
			}

			// 设置Content-Encoding header
			cw.Header().Set("Content-Encoding", string(encoding))
			cw.Header().Add("Vary", "Accept-Encoding")

			defer func() {
				if cw.writer != nil {
					if cw.bufferedWriter != nil {
						cw.bufferedWriter.Flush()
					}
					cw.writer.Close()
				}
			}()

			next.ServeHTTP(cw, r)
		})
	}
}

func (cw *CompressResponseWriter) WriteHeader(statusCode int) {
	if cw.written {
		return
	}

	cw.statusCode = statusCode
	cw.written = true

	// 某些状态码不应该压缩
	if !shouldCompressForStatus(statusCode) {
		cw.compressed = false
		cw.Header().Del("Content-Encoding")
		cw.ResponseWriter.WriteHeader(statusCode)
		return
	}

	// 检查内容类型是否应该压缩
	if !shouldCompressType(cw.Header().Get("Content-Type")) {
		cw.compressed = false
		cw.Header().Del("Content-Encoding")
		cw.ResponseWriter.WriteHeader(statusCode)
		return
	}

	cw.compressed = true
	cw.Header().Del("Content-Length") // 因为内容将被压缩，原长度不再有效
	cw.ResponseWriter.WriteHeader(statusCode)
}

func (cw *CompressResponseWriter) Write(b []byte) (int, error) {
	if !cw.written {
		cw.WriteHeader(http.StatusOK)
	}

	if !cw.compressed {
		return cw.ResponseWriter.Write(b)
	}

	// 延迟初始化压缩写入器
	if cw.writer == nil {
		var err error
		cw.writer, err = cw.compressor.Compress(cw.ResponseWriter)
		if err != nil {
			return 0, err
		}
		cw.bufferedWriter = bufio.NewWriterSize(cw.writer, defaultBufferSize)
	}

	return cw.bufferedWriter.Write(b)
}

// 实现 http.Hijacker 接口
func (cw *CompressResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := cw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// 实现 http.Flusher 接口
func (cw *CompressResponseWriter) Flush() {
	if cw.bufferedWriter != nil {
		cw.bufferedWriter.Flush()
	}
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// 判断是否应该对该状态码的响应进行压缩
func shouldCompressForStatus(status int) bool {
	// 只压缩成功的响应
	return status == http.StatusOK ||
		status == http.StatusCreated ||
		status == http.StatusAccepted ||
		status == http.StatusNonAuthoritativeInfo ||
		status == http.StatusNoContent ||
		status == http.StatusPartialContent
}

// 判断是否应该对该内容类型进行压缩
func shouldCompressType(contentType string) bool {
	// 解析内容类型
	mimeType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	compressibleTypes := map[string]bool{
		"text/":                  true,
		"application/javascript": true,
		"application/json":       true,
		"application/xml":        true,
		"application/x-yaml":     true,
		"image/svg+xml":          true,
	}

	// 检查是否是可压缩类型
	for prefix := range compressibleTypes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}

	return false
}
