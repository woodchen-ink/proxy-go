package middleware

import (
	"fmt"
	"net/http"
	"proxy-go/internal/security"
	"strings"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// SecurityMiddleware 安全中间件
type SecurityMiddleware struct {
	banManager *security.IPBanManager
}

// NewSecurityMiddleware 创建安全中间件
func NewSecurityMiddleware(banManager *security.IPBanManager) *SecurityMiddleware {
	return &SecurityMiddleware{
		banManager: banManager,
	}
}

// isAdminPath 判断是否是管理后台路径
func isAdminPath(path string) bool {
	// 管理后台路径前缀
	adminPrefixes := []string{
		"/admin/",
		"/admin",
	}

	for _, prefix := range adminPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// IPBanMiddleware IP封禁中间件
func (sm *SecurityMiddleware) IPBanMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := iputil.GetClientIP(r)

		// 管理后台路径不受IP封禁限制
		if isAdminPath(r.URL.Path) {
			// 直接放行管理后台请求
			next.ServeHTTP(w, r)
			return
		}

		// 检查IP是否被封禁
		if sm.banManager.IsIPBanned(clientIP) {
			banned, banEndTime := sm.banManager.GetBanInfo(clientIP)
			if banned {
				// 返回429状态码和封禁信息
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", time.Until(banEndTime).Seconds()))
				w.WriteHeader(http.StatusTooManyRequests)

				remainingTime := time.Until(banEndTime)
				response := fmt.Sprintf(`{
					"error": "IP temporarily banned due to excessive 404 errors",
					"message": "您的IP因频繁访问不存在的资源而被暂时封禁",
					"ban_end_time": "%s",
					"remaining_seconds": %.0f
				}`, banEndTime.Format("2006-01-02 15:04:05"), remainingTime.Seconds())

				w.Write([]byte(response))
				return
			}
		}

		// 创建响应写入器包装器来捕获状态码
		wrapper := &responseWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// 继续处理请求
		next.ServeHTTP(wrapper, r)

		// 如果响应是404，记录错误
		if wrapper.statusCode == http.StatusNotFound {
			sm.banManager.RecordError(clientIP)
		}
	})
}

// responseWrapper 响应包装器，用于捕获状态码
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader 重写WriteHeader方法来捕获状态码
func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write 重写Write方法，确保状态码被正确设置
func (rw *responseWrapper) Write(b []byte) (int, error) {
	// 如果还没有设置状态码，默认为200
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}
