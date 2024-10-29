package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// Config 结构体用于解析配置文件
type Config struct {
	MAP map[string]string `json:"MAP"`
}

func main() {
	// 读取配置文件
	configFile, err := os.ReadFile("data/config.json")
	if err != nil {
		log.Fatal("Error reading config file:", err)
	}

	// 解析配置文件
	var config Config
	if err := json.Unmarshal(configFile, &config); err != nil {
		log.Fatal("Error parsing config file:", err)
	}

	// 创建 HTTP 处理函数
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 处理根路径请求
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Welcome to CZL proxy.")
			return
		}

		// 查找匹配的代理路径
		var matchedPrefix string
		var targetBase string
		for prefix, target := range config.MAP {
			if strings.HasPrefix(r.URL.Path, prefix) {
				matchedPrefix = prefix
				targetBase = target
				break
			}
		}

		// 如果没有匹配的路径，返回 404
		if matchedPrefix == "" {
			http.NotFound(w, r)
			return
		}

		// 构建目标 URL
		targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)
		targetURL := targetBase + targetPath

		// 创建新的请求
		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
			return
		}

		// 复制原始请求的 header
		for header, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(header, value)
			}
		}

		// 发送代理请求
		client := &http.Client{}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, "Error forwarding request", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// 复制响应 header
		for header, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(header, value)
			}
		}

		// 设置响应状态码
		w.WriteHeader(resp.StatusCode)

		// 复制响应体
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Printf("Error copying response: %v", err)
		}
	})

	// 启动服务器
	log.Println("Starting proxy server on :80")
	if err := http.ListenAndServe(":80", nil); err != nil {
		log.Fatal("Error starting server:", err)
	}
}
