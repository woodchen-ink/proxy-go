package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"proxy-go/internal/metrics"
	"proxy-go/internal/models"
	"runtime"
	"strings"
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
	TotalBytes         int64                `json:"total_bytes"`
	BytesPerSecond     float64              `json:"bytes_per_second"`
	StatusCodeStats    map[string]int64     `json:"status_code_stats"`
	LatencyPercentiles map[string]float64   `json:"latency_percentiles"`
	TopPaths           []models.PathMetrics `json:"top_paths"`
	RecentRequests     []models.RequestLog  `json:"recent_requests"`
	TopReferers        []models.PathMetrics `json:"top_referers"`
}

func (h *ProxyHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime)
	collector := metrics.GetCollector()
	stats := collector.GetStats()

	if stats == nil {
		// 返回默认值而不是错误
		stats = map[string]interface{}{
			"uptime":              uptime.String(),
			"active_requests":     int64(0),
			"total_requests":      int64(0),
			"total_errors":        int64(0),
			"error_rate":          float64(0),
			"num_goroutine":       runtime.NumGoroutine(),
			"memory_usage":        "0 B",
			"avg_response_time":   "0 ms",
			"total_bytes":         int64(0),
			"bytes_per_second":    float64(0),
			"requests_per_second": float64(0),
			"status_code_stats":   make(map[string]int64),
			"latency_percentiles": make([]float64, 0),
			"top_paths":           make([]models.PathMetrics, 0),
			"recent_requests":     make([]models.RequestLog, 0),
			"top_referers":        make([]models.PathMetrics, 0),
		}
	}

	// 确保所有必要的字段都存在
	metrics := Metrics{
		Uptime:              uptime.String(),
		ActiveRequests:      safeInt64(stats["active_requests"]),
		TotalRequests:       safeInt64(stats["total_requests"]),
		TotalErrors:         safeInt64(stats["total_errors"]),
		ErrorRate:           float64(safeInt64(stats["total_errors"])) / float64(max(safeInt64(stats["total_requests"]), 1)),
		NumGoroutine:        safeInt(stats["num_goroutine"]),
		MemoryUsage:         safeString(stats["memory_usage"]),
		AverageResponseTime: safeString(stats["avg_response_time"]),
		TotalBytes:          safeInt64(stats["total_bytes"]),
		BytesPerSecond:      float64(safeInt64(stats["total_bytes"])) / metrics.Max(uptime.Seconds(), 1),
		RequestsPerSecond:   float64(safeInt64(stats["total_requests"])) / metrics.Max(uptime.Seconds(), 1),
		StatusCodeStats:     safeStatusCodeStats(stats["status_code_stats"]),
		TopPaths:            safePathMetrics(stats["top_paths"]),
		RecentRequests:      safeRequestLogs(stats["recent_requests"]),
		TopReferers:         safePathMetrics(stats["top_referers"]),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		log.Printf("Error encoding metrics: %v", err)
	}
}

// 辅助函数
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
        /* 添加表格样式 */
        table {
            width: 100%;
            border-collapse: collapse;
            margin: 10px 0;
        }
        th, td {
            padding: 8px;
            text-align: left;
            border-bottom: 1px solid #eee;
        }
        th {
            background: #f8f9fa;
            color: #666;
        }
        .status-badge {
            padding: 3px 8px;
            border-radius: 12px;
            font-size: 12px;
            color: white;
        }
        .status-2xx { background: #28a745; }
        .status-3xx { background: #17a2b8; }
        .status-4xx { background: #ffc107; }
        .status-5xx { background: #dc3545; }
        .status-other { background: #000000; }
        .grid-container {
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 20px;
            margin-bottom: 20px;
        }
        .grid-container .card {
            margin-bottom: 0;
        }
        .chart-container {
            margin-top: 20px;
        }
        .chart {
            height: 200px;
            margin-bottom: 50px;
            position: relative;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .chart h3 {
            margin: 0 0 15px 0;
            color: #666;
            font-size: 14px;
        }
        #timeRange {
            padding: 8px;
            border-radius: 4px;
            border: 1px solid #ddd;
            margin-bottom: 15px;
        }
        .time-range-buttons {
            margin: 15px 0;
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
            justify-content: flex-start;
        }
        
        .time-btn {
            padding: 8px 16px;
            border: 1px solid #ddd;
            background: white;
            border-radius: 4px;
            cursor: pointer;
            transition: all 0.3s;
            min-width: 80px;
            text-align: center;
        }
        
        .time-btn:hover {
            background: #f8f9fa;
        }
        
        .time-btn.active {
            background: #007bff;
            color: white;
            border-color: #0056b3;
        }
        .controls {
            display: flex;
            align-items: center;
            gap: 20px;
            margin-bottom: 15px;
            padding: 10px;
            background: #f8f9fa;
            border-radius: 4px;
        }
        
        .controls label {
            display: flex;
            align-items: center;
            gap: 5px;
            font-size: 14px;
            color: #666;
        }
        
        .controls select {
            padding: 5px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
      #statusCodes {
           padding: 15px;
          background: #f8f9fa;
          border-radius: 8px;
      }
      
      .status-row {
          display: flex;
          flex-wrap: wrap;
          gap: 15px;
          justify-content: space-between;
          width: 100%;
       }
       
       .status-item {
           display: flex;
           flex-direction: column;
           align-items: center;
           background: white;
           padding: 10px;
           border-radius: 6px;
           flex: 1;
           min-width: 80px;
           max-width: 120px;
           box-shadow: 0 1px 3px rgba(0,0,0,0.1);
       }
       
       .status-badge {
           text-align: center;
           padding: 4px 12px;
           border-radius: 4px;
           font-size: 12px;
           color: white;
           margin-bottom: 5px;
       }
       
       .metric-value {
           text-align: center;
           font-weight: bold;
           color: #666;
       }
       .loading {
           position: relative;
           opacity: 0.6;
       }
       .loading::after {
           content: "加载中...";
           position: absolute;
           top: 50%;
           left: 50%;
           transform: translate(-50%, -50%);
           background: rgba(255,255,255,0.9);
           padding: 10px 20px;
           border-radius: 4px;
           box-shadow: 0 2px 4px rgba(0,0,0,0.1);
       }
       .error-message {
           position: fixed;
           top: 20px;
           right: 20px;
           background: #dc3545;
           color: white;
           padding: 10px 20px;
           border-radius: 4px;
           box-shadow: 0 2px 4px rgba(0,0,0,0.2);
           z-index: 1000;
           display: none;
       }
       .export-btn {
           padding: 8px 16px;
           background: #28a745;
           color: white;
           border: none;
           border-radius: 4px;
           cursor: pointer;
           transition: all 0.3s;
       }
       .export-btn:hover {
           background: #218838;
       }
    </style>
</head>
<body>
    <h1>Proxy-Go Metrics</h1>

    <div class="grid-container">
        <div class="card">
            <h2>基础指标</h2>
            <div class="metric">
                <span class="metric-label">运行时间</span>
                <span class="metric-value" id="uptime"></span>
            </div>
            <div class="metric">
                <span class="metric-label">当前活跃请求</span>
                <span class="metric-value" id="activeRequests"></span>
            </div>
            <div class="metric">
                <span class="metric-label">总请求数</span>
                <span class="metric-value" id="totalRequests"></span>
            </div>
            <div class="metric">
                <span class="metric-label">错误数</span>
                <span class="metric-value" id="totalErrors"></span>
            </div>
            <div class="metric">
                <span class="metric-label">错误率</span>
                <span class="metric-value" id="errorRate"></span>
            </div>
        </div>

        <div class="card">
            <h2>系统指标</h2>
            <div class="metric">
                <span class="metric-label">Goroutine数量</span>
                <span class="metric-value" id="numGoroutine"></span>
            </div>
            <div class="metric">
                <span class="metric-label">内存使用</span>
                <span class="metric-value" id="memoryUsage"></span>
            </div>
        </div>

        <div class="card">
            <h2>性能指标</h2>
            <div class="metric">
                <span class="metric-label">平均响应时间</span>
                <span class="metric-value" id="avgResponseTime"></span>
            </div>
            <div class="metric">
                <span class="metric-label">每秒请求数</span>
                <span class="metric-value" id="requestsPerSecond"></span>
            </div>
        </div>

        <div class="card">
            <h2>流量统计</h2>
            <div class="metric">
                <span class="metric-label">总传输字节</span>
                <span class="metric-value" id="totalBytes"></span>
            </div>
            <div class="metric">
                <span class="metric-label">每传输</span>
                <span class="metric-value" id="bytesPerSecond"></span>
            </div>
        </div>
    </div>

    <div class="card">
        <h2>状态码统计</h2>
        <div id="statusCodes"></div>
    </div>

    <div class="card">
        <h2>热门路径 (Top 10)</h2>
        <table id="topPaths">
            <thead>
                <tr>
                    <th>路径</th>
                    <th>请求数</th>
                    <th>错误数</th>
                    <th>平均延迟</th>
                    <th>传输大小</th>
                </tr>
            </thead>
            <tbody></tbody>
        </table>
    </div>

    <div class="card">
        <h2>最近请求</h2>
        <table id="recentRequests">
            <thead>
                <tr>
                    <th>时间</th>
                    <th>路径</th>
                    <th>状态</th>
                    <th>延迟</th>
                    <th>大小</th>
                    <th>客户端IP</th>
                </tr>
            </thead>
            <tbody></tbody>
        </table>
    </div>

    <div class="card">
        <h2>热门引用来源 (Top 10)</h2>
        <table id="topReferers">
            <thead>
                <tr>
                    <th>来源</th>
                    <th>请求数</th>
                </tr>
            </thead>
            <tbody></tbody>
        </table>
    </div>

    <span id="lastUpdate"></span>
    <button class="refresh" onclick="refreshMetrics()">刷新</button>

    <div id="errorMessage" class="error-message"></div>

    <script>
        // 检查登录状态
        const token = localStorage.getItem('metricsToken');
        if (!token) {
            window.location.href = '/metrics/ui';
        }

        function formatBytes(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
        }

        function formatDate(dateStr) {
            const date = new Date(dateStr);
            return date.toLocaleTimeString();
        }

        function formatLatency(nanoseconds) {
            if (nanoseconds < 1000) {
                return nanoseconds + ' ns';
            } else if (nanoseconds < 1000000) {
                return (nanoseconds / 1000).toFixed(2) + ' µs';
            } else if (nanoseconds < 1000000000) {
                return (nanoseconds / 1000000).toFixed(2) + ' ms';
            } else {
                return (nanoseconds / 1000000000).toFixed(2) + ' s';
            }
        }

        function updateMetrics(data) {
            // 更新现有指标
            document.getElementById('uptime').textContent = data.uptime;
            document.getElementById('activeRequests').textContent = data.active_requests;
            document.getElementById('totalRequests').textContent = data.total_requests;
            document.getElementById('totalErrors').textContent = data.total_errors;
            document.getElementById('errorRate').textContent = (data.error_rate * 100).toFixed(2) + '%';
            document.getElementById('numGoroutine').textContent = data.num_goroutine;
            document.getElementById('memoryUsage').textContent = data.memory_usage;
            document.getElementById('avgResponseTime').textContent = data.avg_response_time;
            document.getElementById('requestsPerSecond').textContent = data.requests_per_second.toFixed(2);
            
            // 更新流量统计
            document.getElementById('totalBytes').textContent = formatBytes(data.total_bytes);
            document.getElementById('bytesPerSecond').textContent = formatBytes(data.bytes_per_second) + '/s';

            // 更新状态码计
            const statusCodesHtml = '<div class="status-row">' +
                Object.entries(data.status_code_stats || {})
                    .sort((a, b) => a[0].localeCompare(b[0]))
                    .map(([status, count]) => {
                        const firstDigit = status.charAt(0);
                        const statusClass = (firstDigit >= '2' && firstDigit <= '5') 
                            ? 'status-' + firstDigit + 'xx' 
                            : 'status-other';
                        return '<div class="status-item">' +
                            '<span class="status-badge ' + statusClass + '">' + status + '</span>' +
                            '<span class="metric-value">' + count.toLocaleString() + '</span>' +
                            '</div>';
                    }).join('') +
                '</div>';

            const statusCodesContainer = document.getElementById('statusCodes');
            statusCodesContainer.innerHTML = statusCodesHtml;

            // 更新热门路径
            const topPathsHtml = (data.top_paths || []).map(path => 
                '<tr>' +
                    '<td>' + path.path + '</td>' +
                    '<td>' + path.request_count + '</td>' +
                    '<td>' + path.error_count + '</td>' +
                    '<td>' + path.avg_latency + '</td>' +
                    '<td>' + formatBytes(path.bytes_transferred) + '</td>' +
                '</tr>'
            ).join('');
            document.querySelector('#topPaths tbody').innerHTML = topPathsHtml;

            // 更新最近请求
            const recentRequestsHtml = (data.recent_requests || []).map(req => 
                '<tr>' +
                    '<td>' + formatDate(req.Time) + '</td>' +
                    '<td>' + req.Path + '</td>' +
                    '<td><span class="status-badge status-' + Math.floor(req.Status/100) + 'xx">' + req.Status + '</span></td>' +
                    '<td>' + formatLatency(req.Latency) + '</td>' +
                    '<td>' + formatBytes(req.BytesSent) + '</td>' +
                    '<td>' + req.ClientIP + '</td>' +
                '</tr>'
            ).join('');
            document.querySelector('#recentRequests tbody').innerHTML = recentRequestsHtml;

            // 更新热门引用来源
            const topReferersHtml = (data.top_referers || []).map(referer => 
                '<tr>' +
                    '<td>' + referer.path + '</td>' +
                    '<td>' + referer.request_count + '</td>' +
                '</tr>'
            ).join('');
            document.querySelector('#topReferers tbody').innerHTML = topReferersHtml;

            document.getElementById('lastUpdate').textContent = '最后更新: ' + new Date().toLocaleTimeString();
        }

        function showError(message) {
            const errorDiv = document.getElementById('errorMessage');
            errorDiv.textContent = message;
            errorDiv.style.display = 'block';
            setTimeout(() => {
                errorDiv.style.display = 'none';
            }, 3000);
        }

        function refreshMetrics() {
            fetch('/metrics', {
                headers: {
                    'Authorization': 'Bearer ' + token
                }
            })
            .then(response => {
                if (response.status === 401) {
                    localStorage.removeItem('metricsToken');
                    window.location.href = '/metrics/ui';
                    return;
                }
                if (!response.ok) {
                    throw new Error('获取数据失败');
                }
                return response.json();
            })
            .then(data => {
                if (data) updateMetrics(data);
            })
            .catch(error => showError(error.message));
        }

        let refreshTimer;

        function setupAutoRefresh() {
            // 清除已存在的定时器
            if (refreshTimer) {
                clearInterval(refreshTimer);
            }
            // 设置5秒自动刷新
            refreshTimer = setInterval(refreshMetrics, 5 * 1000);  // 改为 5 秒
        }

        document.addEventListener('DOMContentLoaded', function() {
            refreshMetrics(); // 立即加载一次数据
            setupAutoRefresh(); // 设置自动刷新
        });
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

// 添加安全的类型转换辅助函数
func safeStatusCodeStats(v interface{}) map[string]int64 {
	if v == nil {
		return make(map[string]int64)
	}
	if m, ok := v.(map[string]int64); ok {
		return m
	}
	return make(map[string]int64)
}

func safePathMetrics(v interface{}) []models.PathMetrics {
	if v == nil {
		return []models.PathMetrics{}
	}
	if m, ok := v.([]models.PathMetrics); ok {
		return m
	}
	return []models.PathMetrics{}
}

func safeRequestLogs(v interface{}) []models.RequestLog {
	if v == nil {
		return []models.RequestLog{}
	}
	if m, ok := v.([]models.RequestLog); ok {
		return m
	}
	return []models.RequestLog{}
}

func safeInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	if i, ok := v.(int64); ok {
		return i
	}
	return 0
}

func safeInt(v interface{}) int {
	if v == nil {
		return 0
	}
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}

func safeString(v interface{}) string {
	if v == nil {
		return "0 B" // 返回默认值
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "0 B" // 返回默认值
}
