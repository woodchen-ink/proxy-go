package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"proxy-go/internal/service"
	"proxy-go/internal/utils"
	"strings"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	authService *service.AuthService
}

// NewAuthHandler 创建新的认证处理器
func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// CheckAuth 检查认证令牌是否有效
func (h *AuthHandler) CheckAuth(token string) bool {
	return h.authService.ValidateToken(token)
}

// LogoutHandler 处理退出登录请求
func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		log.Printf("[Auth] ERR %s %s -> 401 (%s) no token from %s", r.Method, r.URL.Path, iputil.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	h.authService.RemoveToken(token)

	log.Printf("[Auth] %s %s -> 200 (%s) logout success from %s", r.Method, r.URL.Path, iputil.GetClientIP(r), utils.GetRequestSource(r))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "已退出登录",
	})
}

// AuthMiddleware 认证中间件
func (h *AuthHandler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return h.authService.RequireAuth(next)
}

// LoginHandler 处理登录请求，重定向到 OAuth 授权页面
func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	authURL, err := h.authService.StartOAuthFlow(r)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) OAuth flow start failed: %v from %s", 
			r.Method, r.URL.Path, iputil.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "OAuth configuration error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// OAuthCallbackHandler 处理 OAuth 回调
func (h *AuthHandler) OAuthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// 记录完整请求信息
	log.Printf("[Auth] DEBUG %s %s -> Callback received with state=%s, code=%s, full URL: %s",
		r.Method, r.URL.Path, state, code, r.URL.String())

	// 使用AuthService处理OAuth回调
	result, err := h.authService.HandleOAuthCallback(r, code, state)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) OAuth callback failed: %v from %s", 
			r.Method, r.URL.Path, iputil.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "OAuth callback error", http.StatusInternalServerError)
		return
	}

	if !result.Success {
		log.Printf("[Auth] ERR %s %s -> 400 (%s) OAuth callback failed: %s from %s", 
			r.Method, r.URL.Path, iputil.GetClientIP(r), result.ErrorMessage, utils.GetRequestSource(r))
		http.Error(w, result.ErrorMessage, http.StatusBadRequest)
		return
	}

	// 返回登录成功页面
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<html>
		<head><title>登录成功</title></head>
		<body>
			<script>
				localStorage.setItem('token', '%s');
				localStorage.setItem('user', '%s');
				window.location.href = '/admin/dashboard';
			</script>
		</body>
		</html>
	`, result.Token, result.Username)
}
