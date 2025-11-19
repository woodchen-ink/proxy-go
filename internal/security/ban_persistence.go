package security

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// BanRecord IP封禁历史记录
type BanRecord struct {
	IP           string    `json:"ip"`
	BanTime      time.Time `json:"ban_time"`
	BanEndTime   time.Time `json:"ban_end_time"`
	Reason       string    `json:"reason"`
	ErrorCount   int       `json:"error_count"`
	IsActive     bool      `json:"is_active"`     // 是否当前仍在封禁中
	UnbanTime    time.Time `json:"unban_time,omitempty"` // 解封时间（手动或自动）
	UnbanReason  string    `json:"unban_reason,omitempty"` // 解封原因
}

// BanPersistence IP封禁持久化数据
type BanPersistence struct {
	// 当前被封禁的IP列表
	ActiveBans map[string]BanRecord `json:"active_bans"`
	// 历史封禁记录（包括已解封的）
	History []BanRecord `json:"history"`
	// 最后更新时间
	LastUpdate time.Time `json:"last_update"`
}

// BanStorage IP封禁存储管理器
type BanStorage struct {
	filePath string
	mu       sync.RWMutex
}

// NewBanStorage 创建IP封禁存储管理器
func NewBanStorage(filePath string) *BanStorage {
	return &BanStorage{
		filePath: filePath,
	}
}

// Load 加载封禁数据
func (bs *BanStorage) Load() (*BanPersistence, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	// 检查文件是否存在
	if _, err := os.Stat(bs.filePath); os.IsNotExist(err) {
		// 文件不存在，返回空数据
		return &BanPersistence{
			ActiveBans: make(map[string]BanRecord),
			History:    []BanRecord{},
			LastUpdate: time.Now(),
		}, nil
	}

	// 读取文件
	data, err := os.ReadFile(bs.filePath)
	if err != nil {
		return nil, err
	}

	// 解析JSON
	var persistence BanPersistence
	if err := json.Unmarshal(data, &persistence); err != nil {
		return nil, err
	}

	// 确保map不为nil
	if persistence.ActiveBans == nil {
		persistence.ActiveBans = make(map[string]BanRecord)
	}
	if persistence.History == nil {
		persistence.History = []BanRecord{}
	}

	return &persistence, nil
}

// Save 保存封禁数据
func (bs *BanStorage) Save(persistence *BanPersistence) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	persistence.LastUpdate = time.Now()

	// 序列化为JSON
	data, err := json.MarshalIndent(persistence, "", "  ")
	if err != nil {
		return err
	}

	// 写入临时文件
	tempFile := bs.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	// 重命名为正式文件（原子操作）
	if err := os.Rename(tempFile, bs.filePath); err != nil {
		return err
	}

	log.Printf("[BanStorage] 封禁数据已保存: %d 活跃封禁, %d 历史记录",
		len(persistence.ActiveBans), len(persistence.History))

	return nil
}

// AddBan 添加封禁记录
func (bs *BanStorage) AddBan(ip string, banTime, banEndTime time.Time, reason string, errorCount int) error {
	persistence, err := bs.Load()
	if err != nil {
		return err
	}

	record := BanRecord{
		IP:         ip,
		BanTime:    banTime,
		BanEndTime: banEndTime,
		Reason:     reason,
		ErrorCount: errorCount,
		IsActive:   true,
	}

	// 添加到活跃封禁列表
	persistence.ActiveBans[ip] = record

	// 添加到历史记录
	persistence.History = append(persistence.History, record)

	return bs.Save(persistence)
}

// RemoveBan 移除封禁记录（解封）
func (bs *BanStorage) RemoveBan(ip string, unbanReason string) error {
	persistence, err := bs.Load()
	if err != nil {
		return err
	}

	// 从活跃封禁列表中移除
	record, exists := persistence.ActiveBans[ip]
	if !exists {
		return nil // IP不在封禁列表中
	}

	delete(persistence.ActiveBans, ip)

	// 更新历史记录中的状态
	record.IsActive = false
	record.UnbanTime = time.Now()
	record.UnbanReason = unbanReason

	// 在历史记录中找到对应记录并更新
	for i := len(persistence.History) - 1; i >= 0; i-- {
		if persistence.History[i].IP == ip && persistence.History[i].IsActive {
			persistence.History[i] = record
			break
		}
	}

	return bs.Save(persistence)
}

// CleanupExpired 清理过期的封禁记录
func (bs *BanStorage) CleanupExpired() error {
	persistence, err := bs.Load()
	if err != nil {
		return err
	}

	now := time.Now()
	expiredIPs := []string{}

	// 检查活跃封禁中的过期记录
	for ip, record := range persistence.ActiveBans {
		if now.After(record.BanEndTime) {
			expiredIPs = append(expiredIPs, ip)
		}
	}

	// 移除过期的封禁
	if len(expiredIPs) > 0 {
		for _, ip := range expiredIPs {
			record := persistence.ActiveBans[ip]
			delete(persistence.ActiveBans, ip)

			// 更新历史记录
			record.IsActive = false
			record.UnbanTime = now
			record.UnbanReason = "自动解封（封禁时间已过期）"

			// 在历史记录中找到对应记录并更新
			for i := len(persistence.History) - 1; i >= 0; i-- {
				if persistence.History[i].IP == ip && persistence.History[i].IsActive {
					persistence.History[i] = record
					break
				}
			}
		}

		log.Printf("[BanStorage] 清理了 %d 个过期封禁记录", len(expiredIPs))
		return bs.Save(persistence)
	}

	return nil
}

// GetActiveBans 获取所有活跃的封禁记录
func (bs *BanStorage) GetActiveBans() (map[string]BanRecord, error) {
	persistence, err := bs.Load()
	if err != nil {
		return nil, err
	}

	return persistence.ActiveBans, nil
}

// GetHistory 获取封禁历史记录
func (bs *BanStorage) GetHistory(limit int) ([]BanRecord, error) {
	persistence, err := bs.Load()
	if err != nil {
		return nil, err
	}

	// 如果不限制数量或历史记录少于限制，返回全部
	if limit <= 0 || len(persistence.History) <= limit {
		return persistence.History, nil
	}

	// 返回最近的记录
	start := len(persistence.History) - limit
	return persistence.History[start:], nil
}
