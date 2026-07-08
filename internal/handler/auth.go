package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"proxy-go/internal/service"
	"proxy-go/internal/utils"

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

// LogoutHandler 处理退出登录请求: 吊销会话 token 并清除 cookie, 幂等 (无会话也返回 200)
func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if token := h.authService.TokenFromRequest(r); token != "" {
		h.authService.RemoveToken(token)
	}
	h.authService.ClearAuthCookie(w, r)

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

	// 记录回调到达，避免泄漏 state/code 凭据
	log.Printf("[Auth] DEBUG %s %s -> Callback received (state present=%t, code present=%t)",
		r.Method, r.URL.Path, state != "", code != "")

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

	// 会话 token 走 HttpOnly cookie 下发, 不再经内联脚本落 localStorage
	// (token 不暴露给页面 JS, XSS 拿不到凭据)
	h.authService.SetAuthCookie(w, r, result.Token)
	http.Redirect(w, r, result.RedirectURL, http.StatusSeeOther)
}
