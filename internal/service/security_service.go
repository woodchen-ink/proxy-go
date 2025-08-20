package service

import (
	"proxy-go/internal/security"
	"time"
)

// BannedIPInfo 被封禁IP信息
type BannedIPInfo struct {
	IP               string `json:"ip"`
	BanEndTime       string `json:"ban_end_time"`
	RemainingSeconds int64  `json:"remaining_seconds"`
}

// BannedIPsResponse 被封禁IP列表响应
type BannedIPsResponse struct {
	BannedIPs []BannedIPInfo `json:"banned_ips"`
	Count     int            `json:"count"`
}

// UnbanIPResponse 解封IP响应
type UnbanIPResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// IPStatusResponse IP状态响应
type IPStatusResponse struct {
	IP               string `json:"ip"`
	Banned           bool   `json:"banned"`
	BanEndTime       string `json:"ban_end_time,omitempty"`
	RemainingSeconds int64  `json:"remaining_seconds,omitempty"`
}

type SecurityService struct {
	banManager *security.IPBanManager
}

func NewSecurityService(banManager *security.IPBanManager) *SecurityService {
	return &SecurityService{
		banManager: banManager,
	}
}

// GetBannedIPs 获取被封禁的IP列表
func (s *SecurityService) GetBannedIPs() (*BannedIPsResponse, error) {
	if s.banManager == nil {
		return nil, ErrSecurityManagerNotEnabled
	}

	bannedIPs := s.banManager.GetBannedIPs()

	// 转换为前端友好的格式
	result := make([]BannedIPInfo, 0, len(bannedIPs))
	for ip, banEndTime := range bannedIPs {
		result = append(result, BannedIPInfo{
			IP:               ip,
			BanEndTime:       banEndTime.Format("2006-01-02 15:04:05"),
			RemainingSeconds: int64(time.Until(banEndTime).Seconds()),
		})
	}

	return &BannedIPsResponse{
		BannedIPs: result,
		Count:     len(result),
	}, nil
}

// UnbanIP 手动解封IP
func (s *SecurityService) UnbanIP(ip string) (*UnbanIPResponse, error) {
	if s.banManager == nil {
		return nil, ErrSecurityManagerNotEnabled
	}

	if ip == "" {
		return nil, ErrIPAddressRequired
	}

	success := s.banManager.UnbanIP(ip)

	message := "IP未在封禁列表中"
	if success {
		message = "IP解封成功"
	}

	return &UnbanIPResponse{
		Success: success,
		Message: message,
	}, nil
}

// GetSecurityStats 获取安全统计信息
func (s *SecurityService) GetSecurityStats() (interface{}, error) {
	if s.banManager == nil {
		return nil, ErrSecurityManagerNotEnabled
	}

	return s.banManager.GetStats(), nil
}

// CheckIPStatus 检查IP状态
func (s *SecurityService) CheckIPStatus(ip string, fallbackIP string) (*IPStatusResponse, error) {
	if s.banManager == nil {
		return nil, ErrSecurityManagerNotEnabled
	}

	// 如果没有指定IP，使用fallbackIP
	if ip == "" {
		ip = fallbackIP
	}

	banned, banEndTime := s.banManager.GetBanInfo(ip)

	result := &IPStatusResponse{
		IP:     ip,
		Banned: banned,
	}

	if banned {
		result.BanEndTime = banEndTime.Format("2006-01-02 15:04:05")
		result.RemainingSeconds = int64(time.Until(banEndTime).Seconds())
	}

	return result, nil
}


// 定义错误
var (
	ErrSecurityManagerNotEnabled = NewServiceError("Security manager not enabled")
	ErrIPAddressRequired         = NewServiceError("IP address is required")
)

// ServiceError 服务错误类型
type ServiceError struct {
	Message string
}

func NewServiceError(message string) *ServiceError {
	return &ServiceError{Message: message}
}

func (e *ServiceError) Error() string {
	return e.Message
}