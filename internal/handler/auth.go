package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	ID        interface{} `json:"id,omitempty"` // 使用interface{}以接受数字或字符串
	Email     string      `json:"email,omitempty"`
	Username  string      `json:"username,omitempty"`
	Name      string      `json:"name,omitempty"`
	AvatarURL string      `json:"avatar_url,omitempty"`
	Avatar    string      `json:"avatar,omitempty"` // 添加avatar字段
	Admin     bool        `json:"admin,omitempty"`
	Moderator bool        `json:"moderator,omitempty"`
	Groups    []string    `json:"groups,omitempty"`
	Upstreams interface{} `json:"upstreams,omitempty"` // 添加upstreams字段

	// 添加可能的替代字段名
	Sub           string `json:"sub,omitempty"`
	PreferredName string `json:"preferred_username,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
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
	if os.Getenv("OAUTH_REDIRECT_URI") != "" {
		return os.Getenv("OAUTH_REDIRECT_URI")
	} else {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s/admin/api/oauth/callback", scheme, r.Host)
	}
}

// LoginHandler 处理登录请求，重定向到 OAuth 授权页面
func (h *ProxyHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state := h.auth.generateToken()
	h.auth.states.Store(state, time.Now())

	clientID := os.Getenv("OAUTH_CLIENT_ID")
	redirectURI := getCallbackURL(r)

	authURL := fmt.Sprintf("https://connect.czl.net/oauth2/authorize?%s",
		url.Values{
			"response_type": {"code"},
			"client_id":     {clientID},
			"redirect_uri":  {redirectURI},
			"state":         {state},
		}.Encode())

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// OAuthCallbackHandler 处理 OAuth 回调
func (h *ProxyHandler) OAuthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// 验证 state
	if _, ok := h.auth.states.Load(state); !ok {
		log.Printf("[Auth] ERR %s %s -> 400 (%s) invalid state from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	h.auth.states.Delete(state)

	// 验证code参数
	if code == "" {
		log.Printf("[Auth] ERR %s %s -> 400 (%s) missing code parameter from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Missing code parameter", http.StatusBadRequest)
		return
	}

	// 获取访问令牌
	redirectURI := getCallbackURL(r)
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")

	// 验证OAuth配置
	if clientID == "" || clientSecret == "" {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) missing OAuth credentials from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	resp, err := http.PostForm("https://connect.czl.net/api/oauth2/token",
		url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"client_id":     {clientID},
			"client_secret": {clientSecret},
		})
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to get access token: %v from %s", r.Method, r.URL.Path, utils.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "Failed to get access token", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		log.Printf("[Auth] ERR %s %s -> %d (%s) OAuth server returned error status: %s from %s",
			r.Method, r.URL.Path, resp.StatusCode, utils.GetClientIP(r), resp.Status, utils.GetRequestSource(r))
		http.Error(w, "OAuth server error: "+resp.Status, http.StatusInternalServerError)
		return
	}

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to parse token response: %v from %s", r.Method, r.URL.Path, utils.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
		return
	}

	// 验证访问令牌
	if token.AccessToken == "" {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) received empty access token from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Received invalid token", http.StatusInternalServerError)
		return
	}

	// 获取用户信息
	req, _ := http.NewRequest("GET", "https://connect.czl.net/api/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	client := &http.Client{Timeout: 10 * time.Second}
	userResp, err := client.Do(req)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to get user info: %v from %s", r.Method, r.URL.Path, utils.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer userResp.Body.Close()

	// 检查用户信息响应状态码
	if userResp.StatusCode != http.StatusOK {
		log.Printf("[Auth] ERR %s %s -> %d (%s) userinfo endpoint returned error status: %s from %s",
			r.Method, r.URL.Path, userResp.StatusCode, utils.GetClientIP(r), userResp.Status, utils.GetRequestSource(r))
		http.Error(w, "Failed to get user info: "+userResp.Status, http.StatusInternalServerError)
		return
	}

	// 读取响应体内容并记录
	bodyBytes, err := io.ReadAll(userResp.Body)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to read user info response body: %v from %s",
			r.Method, r.URL.Path, utils.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "Failed to read user info response", http.StatusInternalServerError)
		return
	}

	// 记录响应内容（小心敏感信息）
	log.Printf("[Auth] DEBUG %s %s -> user info response: %s", r.Method, r.URL.Path, string(bodyBytes))

	// 使用更灵活的方式解析JSON
	var rawUserInfo map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawUserInfo); err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to parse raw user info: %v from %s",
			r.Method, r.URL.Path, utils.GetClientIP(r), err, utils.GetRequestSource(r))
		http.Error(w, "Failed to parse user info", http.StatusInternalServerError)
		return
	}

	// 创建用户信息对象
	userInfo := OAuthUserInfo{}

	// 填充用户名（优先级：username > preferred_username > sub > email）
	if username, ok := rawUserInfo["username"].(string); ok && username != "" {
		userInfo.Username = username
	} else if preferred, ok := rawUserInfo["preferred_username"].(string); ok && preferred != "" {
		userInfo.Username = preferred
	} else if sub, ok := rawUserInfo["sub"].(string); ok && sub != "" {
		userInfo.Username = sub
	} else if email, ok := rawUserInfo["email"].(string); ok && email != "" {
		// 从邮箱中提取用户名
		parts := strings.Split(email, "@")
		if len(parts) > 0 {
			userInfo.Username = parts[0]
		}
	}

	// 填充头像URL
	if avatar, ok := rawUserInfo["avatar"].(string); ok && avatar != "" {
		userInfo.Avatar = avatar
	} else if avatarURL, ok := rawUserInfo["avatar_url"].(string); ok && avatarURL != "" {
		userInfo.AvatarURL = avatarURL
	} else if picture, ok := rawUserInfo["picture"].(string); ok && picture != "" {
		userInfo.Picture = picture
	}

	// 验证用户信息
	if userInfo.Username == "" {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) could not extract username from user info from %s",
			r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		http.Error(w, "Invalid user information: missing username", http.StatusInternalServerError)
		return
	}

	// 生成内部访问令牌
	internalToken := h.auth.generateToken()
	h.auth.addToken(internalToken, userInfo.Username, tokenExpiry)

	log.Printf("[Auth] %s %s -> 200 (%s) login success for user %s from %s", r.Method, r.URL.Path, utils.GetClientIP(r), userInfo.Username, utils.GetRequestSource(r))

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
	`, internalToken, userInfo.Username)
}
