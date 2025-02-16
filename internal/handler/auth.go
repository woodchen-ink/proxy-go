package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"proxy-go/internal/utils"
	"strings"
	"sync"
	"time"
)

const (
	tokenExpiry = 30 * 24 * time.Hour // Token 过期时间为 30 天
)

type OAuthUserInfo struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Username  string   `json:"username"`
	Name      string   `json:"name"`
	AvatarURL string   `json:"avatar_url"`
	Admin     bool     `json:"admin"`
	Moderator bool     `json:"moderator"`
	Groups    []string `json:"groups"`
}

type OAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type tokenInfo struct {
	createdAt time.Time
	expiresIn time.Duration
	username  string
}

type authManager struct {
	tokens sync.Map
	states sync.Map
}

func newAuthManager() *authManager {
	am := &authManager{}
	go am.cleanExpiredTokens()
	return am
}

func (am *authManager) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func (am *authManager) addToken(token string, username string, expiry time.Duration) {
	am.tokens.Store(token, tokenInfo{
		createdAt: time.Now(),
		expiresIn: expiry,
		username:  username,
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
		log.Printf("[Auth] ERR %s %s -> 401 (%s) no token from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	h.auth.tokens.Delete(token)

	log.Printf("[Auth] %s %s -> 200 (%s) logout success from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))

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
			log.Printf("[Auth] ERR %s %s -> 401 (%s) no token from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if !h.auth.validateToken(token) {
			log.Printf("[Auth] ERR %s %s -> 401 (%s) invalid token from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// getCallbackURL 从请求中获取回调地址
func getCallbackURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/admin/api/oauth/callback", scheme, r.Host)
}

// LoginHandler 处理登录请求，重定向到 OAuth 授权页面
func (h *ProxyHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state := h.auth.generateToken()
	h.auth.states.Store(state, time.Now())

	clientID := os.Getenv("OAUTH_CLIENT_ID")
	redirectURI := getCallbackURL(r)

	authURL := fmt.Sprintf("https://connect.q58.club/oauth/authorize?%s",
		url.Values{
			"response_type": {"code"},
			"client_id":     {clientID},
			"redirect_uri":  {redirectURI},
			"state":         {state},
		}.Encode())

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// isAllowedUser 检查用户是否在允许列表中
func isAllowedUser(username string) bool {
	allowedUsers := strings.Split(os.Getenv("OAUTH_ALLOWED_USERS"), ",")
	for _, allowed := range allowedUsers {
		if strings.TrimSpace(allowed) == username {
			return true
		}
	}
	return false
}

// OAuthCallbackHandler 处理 OAuth 回调
func (h *ProxyHandler) OAuthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// 验证 state
	if _, ok := h.auth.states.Load(state); !ok {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	h.auth.states.Delete(state)

	// 获取访问令牌
	redirectURI := getCallbackURL(r)
	resp, err := http.PostForm("https://connect.q58.club/api/oauth/access_token",
		url.Values{
			"code":         {code},
			"redirect_uri": {redirectURI},
		})
	if err != nil {
		http.Error(w, "Failed to get access token", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
		return
	}

	// 获取用户信息
	req, _ := http.NewRequest("GET", "https://connect.q58.club/api/oauth/user", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	client := &http.Client{}
	userResp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer userResp.Body.Close()

	var userInfo OAuthUserInfo
	if err := json.NewDecoder(userResp.Body).Decode(&userInfo); err != nil {
		http.Error(w, "Failed to parse user info", http.StatusInternalServerError)
		return
	}

	// 检查用户是否在允许列表中
	if !isAllowedUser(userInfo.Username) {
		http.Error(w, "Unauthorized user", http.StatusUnauthorized)
		return
	}

	// 生成内部访问令牌
	internalToken := h.auth.generateToken()
	h.auth.addToken(internalToken, userInfo.Username, tokenExpiry)

	// 返回登录成功页面
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<html>
		<head><title>登录成功</title></head>
		<body>
			<script>
				localStorage.setItem('token', '%s');
				localStorage.setItem('user', '%s');
				window.location.href = '/admin';
			</script>
		</body>
		</html>
	`, internalToken, userInfo.Username)
}
