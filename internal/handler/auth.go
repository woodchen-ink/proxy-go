package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenInfo struct {
	createdAt time.Time
	expiresIn time.Duration
}

type authManager struct {
	tokens sync.Map
}

func newAuthManager() *authManager {
	am := &authManager{}
	// 启动token清理goroutine
	go am.cleanExpiredTokens()
	return am
}

func (am *authManager) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func (am *authManager) addToken(token string, expiry time.Duration) {
	am.tokens.Store(token, tokenInfo{
		createdAt: time.Now(),
		expiresIn: expiry,
	})
}

func (am *authManager) validateToken(token string) bool {
	if info, ok := am.tokens.Load(token); ok {
		tokenInfo := info.(tokenInfo)
		if time.Since(tokenInfo.createdAt) < tokenInfo.expiresIn {
			return true
		}
		am.tokens.Delete(token)
	}
	return false
}

func (am *authManager) cleanExpiredTokens() {
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		am.tokens.Range(func(key, value interface{}) bool {
			token := key.(string)
			info := value.(tokenInfo)
			if time.Since(info.createdAt) >= info.expiresIn {
				am.tokens.Delete(token)
			}
			return true
		})
	}
}

// CheckAuth 检查认证令牌是否有效
func (h *ProxyHandler) CheckAuth(token string) bool {
	return h.auth.validateToken(token)
}

// LogoutHandler 处理退出登录请求
func (h *ProxyHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	h.auth.tokens.Delete(token)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "已退出登录",
	})
}

// AuthMiddleware 认证中间件
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

// AuthHandler 处理认证请求
func (h *ProxyHandler) AuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[Auth] 方法不允许: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单数据
	if err := r.ParseForm(); err != nil {
		log.Printf("[Auth] 表单解析失败: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	log.Printf("[Auth] 收到登录请求，密码长度: %d", len(password))

	if password == "" {
		log.Printf("[Auth] 密码为空")
		http.Error(w, "Password is required", http.StatusBadRequest)
		return
	}

	if password != h.config.Metrics.Password {
		log.Printf("[Auth] 密码错误")
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token := h.auth.generateToken()
	h.auth.addToken(token, time.Duration(h.config.Metrics.TokenExpiry)*time.Second)

	log.Printf("[Auth] 登录成功，生成令牌")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"token": token,
	})
}
