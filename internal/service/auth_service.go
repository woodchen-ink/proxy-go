package service

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

	"github.com/woodchen-ink/go-web-utils/iputil"
)

const (
	tokenExpiry = 30 * 24 * time.Hour // Token 过期时间为 30 天
	stateExpiry = 10 * time.Minute    // State 过期时间为 10 分钟
)

// AuthResult 认证结果
type AuthResult struct {
	Success      bool
	RedirectURL  string
	ErrorMessage string
	Token        string
	Username     string
}

// OAuthUserInfo OAuth用户信息
type OAuthUserInfo struct {
	ID        interface{} `json:"id,omitempty"`
	Email     string      `json:"email,omitempty"`
	Username  string      `json:"username,omitempty"`
	Name      string      `json:"name,omitempty"`
	AvatarURL string      `json:"avatar_url,omitempty"`
	Avatar    string      `json:"avatar,omitempty"`
	Admin     bool        `json:"admin,omitempty"`
	Moderator bool        `json:"moderator,omitempty"`
	Groups    []string    `json:"groups,omitempty"`
	Upstreams interface{} `json:"upstreams,omitempty"`

	Sub           string `json:"sub,omitempty"`
	PreferredName string `json:"preferred_username,omitempty"`
	GivenName     string `json:"given_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
}

// OAuthToken OAuth令牌响应
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type tokenInfo struct {
	createdAt time.Time
	expiresIn time.Duration
	username  string
}

type stateInfo struct {
	createdAt time.Time
	expiresAt time.Time
}

// OAuthConfig OAuth配置
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AuthURL      string
	TokenURL     string
}

// AuthService 认证服务
type AuthService struct {
	oauthConfig *OAuthConfig
	sessionKey  string
	tokens      sync.Map
	states      sync.Map
}

func NewAuthService(oauthConfig *OAuthConfig) *AuthService {
	service := &AuthService{
		oauthConfig: oauthConfig,
		sessionKey:  "authenticated",
	}
	go service.cleanExpiredTokens()
	go service.cleanExpiredStates()
	return service
}

// NewAuthServiceFromEnv 从环境变量创建AuthService
func NewAuthServiceFromEnv() *AuthService {
	var oauthConfig *OAuthConfig

	// 从环境变量获取OAuth配置
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")

	// 添加调试日志
	log.Printf("[Auth] DEBUG OAuth环境变量检查: clientID=%s, clientSecret=%s",
		func() string {
			if clientID != "" {
				return "已设置"
			} else {
				return "未设置"
			}
		}(),
		func() string {
			if clientSecret != "" {
				return "已设置"
			} else {
				return "未设置"
			}
		}())

	if clientID != "" && clientSecret != "" {
		oauthConfig = &OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURI:  os.Getenv("OAUTH_REDIRECT_URI"),
			AuthURL:      "https://connect.czl.net/oauth2/authorize",
			TokenURL:     "https://connect.czl.net/api/oauth2/token",
		}
	} else {
		log.Printf("[Auth] WARNING OAuth配置未完整设置，需要设置OAUTH_CLIENT_ID和OAUTH_CLIENT_SECRET环境变量")
	}

	service := &AuthService{
		oauthConfig: oauthConfig,
		sessionKey:  "authenticated",
	}
	go service.cleanExpiredTokens()
	go service.cleanExpiredStates()
	return service
}

// ValidateToken 验证认证令牌
func (s *AuthService) ValidateToken(token string) bool {
	if info, ok := s.tokens.Load(token); ok {
		tokenInfo := info.(tokenInfo)
		if time.Since(tokenInfo.createdAt) < tokenInfo.expiresIn {
			return true
		}
		s.tokens.Delete(token)
	}
	return false
}

// IsAuthenticated 检查用户是否已认证
func (s *AuthService) IsAuthenticated(r *http.Request) bool {
	// 检查Authorization头
	auth := r.Header.Get("Authorization")
	if auth != "" && strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return s.ValidateToken(token)
	}

	// 检查 session cookie
	cookie, err := r.Cookie(s.sessionKey)
	if err != nil {
		return false
	}

	// 简单验证 - 实际应用中应该验证JWT或session store
	return cookie.Value == "true"
}

// generateToken 生成随机令牌
func (s *AuthService) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// GenerateState 生成OAuth state参数
func (s *AuthService) GenerateState() string {
	state := s.generateToken()
	s.states.Store(state, stateInfo{
		createdAt: time.Now(),
		expiresAt: time.Now().Add(stateExpiry),
	})
	return state
}

// ValidateState 验证OAuth state参数
func (s *AuthService) ValidateState(state string) bool {
	if info, ok := s.states.Load(state); ok {
		stateInfo := info.(stateInfo)
		if time.Now().Before(stateInfo.expiresAt) {
			s.states.Delete(state) // 使用后立即删除
			return true
		}
		s.states.Delete(state) // 过期也删除
	}
	return false
}

// AddToken 添加认证令牌
func (s *AuthService) AddToken(username string) string {
	token := s.generateToken()
	s.tokens.Store(token, tokenInfo{
		createdAt: time.Now(),
		expiresIn: tokenExpiry,
		username:  username,
	})
	return token
}

// RemoveToken 移除认证令牌
func (s *AuthService) RemoveToken(token string) {
	s.tokens.Delete(token)
}

// cleanExpiredTokens 清理过期令牌
func (s *AuthService) cleanExpiredTokens() {
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		s.tokens.Range(func(key, value interface{}) bool {
			token := key.(string)
			info := value.(tokenInfo)
			if time.Since(info.createdAt) >= info.expiresIn {
				s.tokens.Delete(token)
			}
			return true
		})
	}
}

// cleanExpiredStates 清理过期状态
func (s *AuthService) cleanExpiredStates() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		s.states.Range(func(key, value interface{}) bool {
			state := key.(string)
			info := value.(stateInfo)
			if time.Now().After(info.expiresAt) {
				s.states.Delete(state)
			}
			return true
		})
	}
}

// StartOAuthFlow 启动OAuth认证流程
func (s *AuthService) StartOAuthFlow(r *http.Request) (string, error) {
	if s.oauthConfig == nil {
		return "", fmt.Errorf("OAuth not configured")
	}

	state := s.GenerateState()
	redirectURI := s.getCallbackURL(r)

	// 记录生成的state和重定向URI
	log.Printf("[Auth] DEBUG %s %s -> Generated state=%s, redirect_uri=%s",
		r.Method, r.URL.Path, state, redirectURI)

	authURL := fmt.Sprintf("%s?%s", s.oauthConfig.AuthURL,
		url.Values{
			"response_type": {"code"},
			"client_id":     {s.oauthConfig.ClientID},
			"redirect_uri":  {redirectURI},
			"scope":         {"read write"},
			"state":         {state},
		}.Encode())

	return authURL, nil
}

// getCallbackURL 从请求中获取回调地址
func (s *AuthService) getCallbackURL(r *http.Request) string {
	if s.oauthConfig.RedirectURI != "" {
		// 验证URI格式
		if _, err := url.Parse(s.oauthConfig.RedirectURI); err == nil {
			log.Printf("[Auth] DEBUG Using configured OAUTH_REDIRECT_URI: %s", s.oauthConfig.RedirectURI)
			return s.oauthConfig.RedirectURI
		}
		log.Printf("[Auth] WARNING Invalid OAUTH_REDIRECT_URI format: %s", s.oauthConfig.RedirectURI)
	}

	// 更可靠地检测协议
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	// 考虑X-Forwarded-Host头
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}

	callbackURL := fmt.Sprintf("%s://%s/admin/api/oauth/callback", scheme, host)
	log.Printf("[Auth] DEBUG Generated callback URL: %s", callbackURL)
	return callbackURL
}

// HandleOAuthCallback 处理OAuth回调
func (s *AuthService) HandleOAuthCallback(r *http.Request, code, state string) (*AuthResult, error) {
	if s.oauthConfig == nil {
		return &AuthResult{
			Success:      false,
			ErrorMessage: "OAuth not configured",
		}, nil
	}

	// 验证 state
	if !s.ValidateState(state) {
		log.Printf("[Auth] ERR %s %s -> 400 (%s) invalid state '%s' from %s",
			r.Method, r.URL.Path, iputil.GetClientIP(r), state, utils.GetRequestSource(r))
		return &AuthResult{
			Success:      false,
			ErrorMessage: "Invalid state",
		}, nil
	}

	// 验证code参数
	if code == "" {
		log.Printf("[Auth] ERR %s %s -> 400 (%s) missing code parameter from %s",
			r.Method, r.URL.Path, iputil.GetClientIP(r), utils.GetRequestSource(r))
		return &AuthResult{
			Success:      false,
			ErrorMessage: "Missing code parameter",
		}, nil
	}

	// 获取访问令牌
	redirectURI := s.getCallbackURL(r)

	// 记录令牌交换请求信息
	log.Printf("[Auth] DEBUG %s %s -> Exchanging code for token with redirect_uri=%s",
		r.Method, r.URL.Path, redirectURI)

	// 交换code为token
	token, err := s.exchangeCodeForToken(code, redirectURI)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to get access token: %v from %s",
			r.Method, r.URL.Path, iputil.GetClientIP(r), err, utils.GetRequestSource(r))
		return &AuthResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to get access token: %v", err),
		}, nil
	}

	// 获取用户信息
	userInfo, err := s.getUserInfo(token.AccessToken)
	if err != nil {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) failed to get user info: %v from %s",
			r.Method, r.URL.Path, iputil.GetClientIP(r), err, utils.GetRequestSource(r))
		return &AuthResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to get user info: %v", err),
		}, nil
	}

	// 验证用户信息
	if userInfo.Username == "" {
		log.Printf("[Auth] ERR %s %s -> 500 (%s) could not extract username from user info from %s",
			r.Method, r.URL.Path, iputil.GetClientIP(r), utils.GetRequestSource(r))
		return &AuthResult{
			Success:      false,
			ErrorMessage: "Invalid user information: missing username",
		}, nil
	}

	// 生成内部访问令牌
	internalToken := s.AddToken(userInfo.Username)

	log.Printf("[Auth] %s %s -> 200 (%s) login success for user %s from %s",
		r.Method, r.URL.Path, iputil.GetClientIP(r), userInfo.Username, utils.GetRequestSource(r))

	return &AuthResult{
		Success:     true,
		RedirectURL: "/admin/dashboard",
		Token:       internalToken,
		Username:    userInfo.Username,
	}, nil
}

// SetAuthCookie 设置认证cookie
func (s *AuthService) SetAuthCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     s.sessionKey,
		Value:    "true",
		Path:     "/",
		HttpOnly: true,
		Secure:   true, // 生产环境应该为true
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(24 * time.Hour),
	}
	http.SetCookie(w, cookie)
}

// ClearAuthCookie 清除认证cookie
func (s *AuthService) ClearAuthCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     s.sessionKey,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
	}
	http.SetCookie(w, cookie)
}

// exchangeCodeForToken 交换授权码为访问令牌
func (s *AuthService) exchangeCodeForToken(code, redirectURI string) (*OAuthToken, error) {
	resp, err := http.PostForm(s.oauthConfig.TokenURL,
		url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"client_id":     {s.oauthConfig.ClientID},
			"client_secret": {s.oauthConfig.ClientSecret},
		})
	if err != nil {
		return nil, fmt.Errorf("failed to post token request: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OAuth server returned error %d: %s, response: %s",
			resp.StatusCode, resp.Status, string(bodyBytes))
	}

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %v", err)
	}

	// 验证访问令牌
	if token.AccessToken == "" {
		return nil, fmt.Errorf("received empty access token")
	}

	return &token, nil
}

// getUserInfo 获取用户信息
func (s *AuthService) getUserInfo(accessToken string) (*OAuthUserInfo, error) {
	req, _ := http.NewRequest("GET", "https://connect.czl.net/api/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 10 * time.Second}

	userResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer userResp.Body.Close()

	// 检查用户信息响应状态码
	if userResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned error status: %s", userResp.Status)
	}

	// 读取响应体内容
	bodyBytes, err := io.ReadAll(userResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info response body: %v", err)
	}

	// 记录响应内容（小心敏感信息）
	log.Printf("[Auth] DEBUG user info response: %s", string(bodyBytes))

	// 使用更灵活的方式解析JSON
	var rawUserInfo map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawUserInfo); err != nil {
		return nil, fmt.Errorf("failed to parse raw user info: %v", err)
	}

	// 创建用户信息对象
	userInfo := &OAuthUserInfo{}

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

	return userInfo, nil
}

// RequireAuth 中间件：要求认证
func (s *AuthService) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.IsAuthenticated(r) {
			// 如果是API请求，返回401
			if strings.HasPrefix(r.URL.Path, "/admin/api/") {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "Authentication required",
				})
				return
			}

			// 重定向到登录页面
			http.Redirect(w, r, "/admin/auth", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}
