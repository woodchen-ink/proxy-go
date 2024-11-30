package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

type Metrics struct {
	// 基础指标
	Uptime         string  `json:"uptime"`
	ActiveRequests int64   `json:"active_requests"`
	TotalRequests  int64   `json:"total_requests"`
	TotalErrors    int64   `json:"total_errors"`
	ErrorRate      float64 `json:"error_rate"`

	// 系统指标
	NumGoroutine int    `json:"num_goroutine"`
	MemoryUsage  string `json:"memory_usage"`

	// 性能指标
	AverageResponseTime string  `json:"avg_response_time"`
	RequestsPerSecond   float64 `json:"requests_per_second"`

	// 新增字段
	TotalBytes         int64              `json:"total_bytes"`
	BytesPerSecond     float64            `json:"bytes_per_second"`
	StatusCodeStats    map[string]int64   `json:"status_code_stats"`
	LatencyPercentiles map[string]float64 `json:"latency_percentiles"`
	TopPaths           []PathMetrics      `json:"top_paths"`
	RecentRequests     []RequestLog       `json:"recent_requests"`
}

type PathMetrics struct {
	Path             string `json:"path"`
	RequestCount     int64  `json:"request_count"`
	ErrorCount       int64  `json:"error_count"`
	AvgLatency       string `json:"avg_latency"`
	BytesTransferred int64  `json:"bytes_transferred"`
}

// 添加格式化字节的辅助函数
func formatBytes(bytes uint64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}

func (h *ProxyHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取状态码统计
	statusStats := make(map[string]int64)
	for i, v := range h.metrics.statusStats {
		statusStats[fmt.Sprintf("%dxx", i+1)] = v.Load()
	}

	// 获取Top 10路径统计
	var pathMetrics []PathMetrics
	h.metrics.pathStats.Range(func(key, value interface{}) bool {
		stats := value.(*PathStats)
		pathMetrics = append(pathMetrics, PathMetrics{
			Path:             key.(string),
			RequestCount:     stats.requests.Load(),
			ErrorCount:       stats.errors.Load(),
			AvgLatency:       formatDuration(time.Duration(stats.latencySum.Load() / stats.requests.Load())),
			BytesTransferred: stats.bytes.Load(),
		})
		return len(pathMetrics) < 10
	})

	// 获取最近的请求
	var recentReqs []RequestLog
	h.recentRequests.RLock()
	cursor := h.recentRequests.cursor.Load()
	for i := 0; i < 10; i++ {
		idx := (cursor - int64(i) + 1000) % 1000
		if h.recentRequests.items[idx] != nil {
			recentReqs = append(recentReqs, *h.recentRequests.items[idx])
		}
	}
	h.recentRequests.RUnlock()

	metrics := Metrics{
		Uptime:          time.Since(h.startTime).String(),
		ActiveRequests:  atomic.LoadInt64(&h.metrics.activeRequests),
		TotalRequests:   atomic.LoadInt64(&h.metrics.totalRequests),
		TotalErrors:     atomic.LoadInt64(&h.metrics.totalErrors),
		ErrorRate:       float64(h.metrics.totalErrors) / float64(h.metrics.totalRequests),
		NumGoroutine:    runtime.NumGoroutine(),
		MemoryUsage:     formatBytes(m.Alloc),
		TotalBytes:      h.metrics.totalBytes.Load(),
		BytesPerSecond:  float64(h.metrics.totalBytes.Load()) / time.Since(h.startTime).Seconds(),
		StatusCodeStats: statusStats,
		TopPaths:        pathMetrics,
		RecentRequests:  recentReqs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// 添加格式化时间的辅助函数
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2f μs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Milliseconds()))
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

// 修改模板,添加登录页面
var loginTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Proxy-Go Metrics Login</title>
    <meta charset="UTF-8">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background: #f5f5f5;
        }
        .login-card {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            width: 300px;
        }
        .login-title {
            text-align: center;
            margin-bottom: 20px;
            color: #333;
        }
        .input-group {
            margin-bottom: 15px;
        }
        input {
            width: 100%;
            padding: 8px;
            border: 1px solid #ddd;
            border-radius: 4px;
            box-sizing: border-box;
        }
        button {
            width: 100%;
            padding: 10px;
            background: #4CAF50;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        button:hover {
            background: #45a049;
        }
        .error {
            color: red;
            text-align: center;
            margin-bottom: 15px;
            display: none;
        }
    </style>
</head>
<body>
    <div class="login-card">
        <h2 class="login-title">Metrics Login</h2>
        <div id="error" class="error">密码错误</div>
        <div class="input-group">
            <input type="password" id="password" placeholder="请输入密码">
        </div>
        <button onclick="login()">登录</button>
    </div>

    <script>
        function login() {
            const password = document.getElementById('password').value;
            fetch('/metrics/auth', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({password: password})
            })
            .then(response => {
                if (response.ok) {
                    // 登录成功,保存token并跳转
                    response.json().then(data => {
                        localStorage.setItem('metricsToken', data.token);
                        window.location.href = '/metrics/dashboard';
                    });
                } else {
                    // 显示错误信息
                    document.getElementById('error').style.display = 'block';
                }
            })
            .catch(error => {
                console.error('Error:', error);
                document.getElementById('error').style.display = 'block';
            });
        }
    </script>
</body>
</html>
`

// 修改原有的 metricsTemplate,添加 token 检查
var metricsTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Proxy-Go Metrics</title>
    <meta charset="UTF-8">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .card {
            background: white;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .metric {
            display: flex;
            justify-content: space-between;
            padding: 10px 0;
            border-bottom: 1px solid #eee;
        }
        .metric:last-child {
            border-bottom: none;
        }
        .metric-label {
            color: #666;
        }
        .metric-value {
            font-weight: bold;
            color: #333;
        }
        h1 {
            color: #333;
            margin-bottom: 30px;
        }
        h2 {
            color: #666;
            margin: 0 0 15px 0;
        }
        .refresh {
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 10px 20px;
            background: #4CAF50;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        .refresh:hover {
            background: #45a049;
        }
        #lastUpdate {
            position: fixed;
            top: 20px;
            right: 140px;
            color: #666;
        }
    </style>
</head>
<body>
    <script>
        // 检查登录状态
        const token = localStorage.getItem('metricsToken');
        if (!token) {
            window.location.href = '/metrics/ui';
        }

        function refreshMetrics() {
            fetch('/metrics', {
                headers: {
                    'Authorization': 'Bearer ' + token
                }
            })
            .then(response => {
                if (response.status === 401) {
                    // token 无效,跳转到登录页
                    localStorage.removeItem('metricsToken');
                    window.location.href = '/metrics/ui';
                    return;
                }
                return response.json();
            })
            .then(data => {
                if (data) updateMetrics(data);
            })
            .catch(error => console.error('Error:', error));
        }

        function updateMetrics(data) {
            document.getElementById('uptime').textContent = data.uptime;
            document.getElementById('activeRequests').textContent = data.active_requests;
            document.getElementById('totalRequests').textContent = data.total_requests;
            document.getElementById('totalErrors').textContent = data.total_errors;
            document.getElementById('errorRate').textContent = (data.error_rate * 100).toFixed(2) + '%';
            document.getElementById('numGoroutine').textContent = data.num_goroutine;
            document.getElementById('memoryUsage').textContent = data.memory_usage;
            document.getElementById('avgResponseTime').textContent = data.avg_response_time;
            document.getElementById('requestsPerSecond').textContent = data.requests_per_second.toFixed(2);
            document.getElementById('lastUpdate').textContent = '最后更新: ' + new Date().toLocaleTimeString();
        }

        // 初始加载
        refreshMetrics();

        // 每5秒自动刷新
        setInterval(refreshMetrics, 5000);
    </script>
</body>
</html>
`

// 添加认证中间件
func (h *ProxyHandler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if !h.auth.validateToken(token) {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// 修改处理器
func (h *ProxyHandler) MetricsPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(loginTemplate))
}

func (h *ProxyHandler) MetricsDashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(metricsTemplate))
}

func (h *ProxyHandler) MetricsAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Password != h.config.Metrics.Password {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token := h.auth.generateToken()
	h.auth.addToken(token, time.Duration(h.config.Metrics.TokenExpiry)*time.Second)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": token,
	})
}
