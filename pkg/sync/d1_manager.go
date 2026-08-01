package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// D1Manager D1同步管理器（简化版本，不支持目录同步）
type D1Manager struct {
	storage      *D1Client
	configLoader ConfigLoader
	status       atomic.Value // SyncStatus
	stopChan     chan struct{}
	eventChan    chan SyncEvent

	// configTrusted 标记"本进程内存里的配置是否可信"
	//
	// 只有确认过远端状态 (成功拉取, 或确认远端为空) 才置真。启动时拉取失败会
	// 退回本地兜底配置 —— 那份配置可能只是仓库自带的样例, 绝不能拿它去覆盖线上。
	// 由于 UploadConfig 会删除本地不存在的路径, 一次误传就会抹掉整个线上配置,
	// 所以不可信时必须禁止一切上传
	configTrusted atomic.Bool
}

// NewD1Manager 创建D1同步管理器
func NewD1Manager(storage *D1Client, configLoader ConfigLoader) *D1Manager {
	m := &D1Manager{
		storage:      storage,
		configLoader: configLoader,
		stopChan:     make(chan struct{}),
		eventChan:    make(chan SyncEvent, 100),
	}

	// 初始化状态
	m.status.Store(SyncStatus{
		IsRunning: false,
	})

	return m
}

// Start 启动同步服务
func (m *D1Manager) Start(ctx context.Context) error {
	status := m.status.Load().(SyncStatus)
	if status.IsRunning {
		return fmt.Errorf("D1 sync manager already running")
	}

	// 更新状态
	status.IsRunning = true
	m.status.Store(status)

	// 发送启动事件
	m.sendEvent(SyncEvent{
		Type:      SyncEventStart,
		Timestamp: time.Now(),
		Message:   "D1 sync manager started",
	})

	log.Printf("[D1Sync] Skipping initial sync at startup (already downloaded config)")

	// 注意: 不再需要定时同步循环
	// - Config: 通过 ConfigUpdateCallback 在修改时立即同步
	// - Path Stats/Metrics: 由 MetricsStorage 每 30 分钟自动同步
	// - Banned IPs: 在封禁/解封时立即同步

	return nil
}

// Stop 停止同步服务
func (m *D1Manager) Stop() error {
	status := m.status.Load().(SyncStatus)
	if !status.IsRunning {
		return fmt.Errorf("D1 sync manager not running")
	}

	close(m.stopChan)

	// 更新状态
	status.IsRunning = false
	m.status.Store(status)

	return nil
}

// SyncNow 立即同步所有数据
func (m *D1Manager) SyncNow(ctx context.Context) error {
	log.Println("[D1Sync] Starting full data sync...")

	// 更新状态
	status := m.status.Load().(SyncStatus)
	status.LastSync = time.Now()

	// 同步配置
	if err := m.syncConfigToD1(ctx); err != nil {
		status.LastError = err.Error()
		m.status.Store(status)

		m.sendEvent(SyncEvent{
			Type:      SyncEventError,
			Timestamp: time.Now(),
			Message:   "Config sync failed",
			Error:     err,
		})

		return err
	}

	// 注意: Path Stats 和 Banned IPs 已通过各自的服务直接同步到 D1
	// - Path Stats: 由 MetricsStorage 按 saveInterval 定期合并上报
	// - Banned IPs: 在封禁发生时立即同步
	// 这里不再从本地文件读取同步（D1-only 模式）

	// 获取本地版本信息
	localVersion := m.configLoader.GetConfigVersion()
	status.LocalVersion = localVersion
	status.RemoteVersion = localVersion // 同步后版本应该一致

	// 清除错误
	status.LastError = ""
	m.status.Store(status)

	m.sendEvent(SyncEvent{
		Type:      SyncEventSuccess,
		Timestamp: time.Now(),
		Message:   "Full D1 sync completed successfully",
	})

	log.Println("[D1Sync] Full sync completed")
	return nil
}

// UploadConfig 上传配置（转换为列式存储）
func (m *D1Manager) UploadConfig(ctx context.Context, config any) error {
	// 保存配置到临时文件以便转换
	tempFile := "data/config.json.temp"
	configJson, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(tempFile, configJson, 0644); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	defer os.Remove(tempFile)

	// 转换为列式存储格式
	maps, others, err := ConvertConfigFromFile(tempFile)
	if err != nil {
		return fmt.Errorf("failed to convert config: %w", err)
	}

	// 获取 D1 中现有的所有路径
	existingMaps, err := m.storage.GetConfigMaps(ctx, false)
	if err != nil {
		log.Printf("[D1Sync] Warning: failed to get existing config maps: %v", err)
		// 继续执行,不影响上传
	} else {
		// 构建新配置中的路径集合
		newPaths := make(map[string]bool)
		for _, m := range maps {
			newPaths[m.Path] = true
		}

		// 删除 D1 中已不存在的路径
		deletedCount := 0
		for _, existing := range existingMaps {
			if !newPaths[existing.Path] {
				if err := m.storage.DeleteConfigMap(ctx, existing.Path); err != nil {
					log.Printf("[D1Sync] Warning: failed to delete config map %s: %v", existing.Path, err)
				} else {
					deletedCount++
					log.Printf("[D1Sync] Deleted config map: %s", existing.Path)
				}
			}
		}
		if deletedCount > 0 {
			log.Printf("[D1Sync] Deleted %d config maps", deletedCount)
		}
	}

	// 批量上传 ConfigMaps
	if len(maps) > 0 {
		if err := m.storage.BatchUpsertConfigMaps(ctx, maps); err != nil {
			return fmt.Errorf("failed to upload config maps: %w", err)
		}
		log.Printf("[D1Sync] Uploaded %d config maps", len(maps))
	}

	// 批量上传 ConfigOther, 顺带写入配置版本号 (心跳下行的判据, 见 pulse.go)
	//
	// 版本号必须显式维护而不是取 MAX(updated_at): 删除路径会让 MAX 回退, 版本必须单调
	version := time.Now().UnixMilli()
	others = append(others, ConfigOther{
		Key:       configVersionKey,
		Value:     fmt.Sprintf("%d", version),
		UpdatedAt: version,
	})
	if err := m.storage.BatchUpsertConfigOther(ctx, others); err != nil {
		return fmt.Errorf("failed to upload config other: %w", err)
	}
	log.Printf("[D1Sync] Uploaded %d other configs (version=%d)", len(others)-1, version)

	// 本进程持有的配置已是这个版本, 心跳不必再回灌一遍
	AdvanceConfigCursor(version)

	return nil
}

// GetSyncStatus 获取同步状态
func (m *D1Manager) GetSyncStatus() SyncStatus {
	return m.status.Load().(SyncStatus)
}

// GetEventChannel 获取事件通道
func (m *D1Manager) GetEventChannel() <-chan SyncEvent {
	return m.eventChan
}

// syncConfigToD1 同步配置到D1
//
// 配置不可信时直接跳过: 见 configTrusted 字段说明。这条守卫覆盖了所有上传入口
// (配置修改回调 / 关闭前的最终同步 / 手动 SyncNow), 是防止兜底配置覆盖线上的最后一道闸
func (m *D1Manager) syncConfigToD1(ctx context.Context) error {
	if !m.configTrusted.Load() {
		log.Printf("[D1Sync] !!! 跳过配置上传: 本进程未能从 D1 成功拉取过配置, 内存中的配置不可信")
		return nil
	}

	config, err := m.configLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load local config: %w", err)
	}

	return m.UploadConfig(ctx, config)
}

// MarkConfigTrusted 在确认已拿到远端配置后放开上传
//
// 供后台「从 D1 拉取」成功后调用, 让只读模式下的节点无需重启即可恢复配置同步
func (m *D1Manager) MarkConfigTrusted() {
	if m.configTrusted.CompareAndSwap(false, true) {
		log.Printf("[D1Sync] 配置已与 D1 对齐, 恢复配置同步")
	}
}

// fetchRemoteConfigWithRetry 拉取远端配置, 失败重试
//
// 只要有一次成功就返回。502 / 超时这类瞬时故障靠重试基本能躲过去 ——
// 而一次误判"远端没有配置"的代价是整个线上配置被覆盖, 值得多等这几秒
func (m *D1Manager) fetchRemoteConfigWithRetry(ctx context.Context) ([]ConfigMap, []ConfigOther, error) {
	const attempts = 3
	var lastErr error

	for i := 0; i < attempts; i++ {
		if i > 0 {
			delay := time.Duration(i*i) * time.Second // 0s / 1s / 4s
			log.Printf("[D1Sync] 第 %d 次拉取配置失败 (%v), %v 后重试", i, lastErr, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}

		maps, mapsErr := m.storage.GetConfigMaps(ctx, false)
		if mapsErr != nil {
			lastErr = mapsErr
			continue
		}
		others, othersErr := m.storage.GetConfigOther(ctx, "")
		if othersErr != nil {
			lastErr = othersErr
			continue
		}
		return maps, others, nil
	}

	return nil, nil, lastErr
}

// toGenericMap 把任意配置对象过一遍 JSON 转成 map[string]any
func toGenericMap(v any) (map[string]any, error) {
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// downloadConfigWithFallback 下载配置，如果远程不存在则上传本地配置
// 返回: 远程配置数据（如果存在），是否使用了本地配置，错误
func (m *D1Manager) downloadConfigWithFallback(ctx context.Context) (map[string]any, bool, error) {
	log.Printf("[D1Sync] Checking for remote config...")

	maps, others, fetchErr := m.fetchRemoteConfigWithRetry(ctx)

	// "拉取失败" 与 "远端确实是空的" 必须区分对待, 这是数据安全的关键分界:
	//
	//   - 远端确实为空: 我们**知道**远端状态, 上传本地配置作为初始版本是安全的
	//   - 拉取失败:     我们**不知道**远端有什么。此时上传本地配置等于拿一份可能是
	//                   兜底样例的配置去覆盖线上 —— 而 UploadConfig 会删除本地不存在
	//                   的路径, 后果是线上配置被整体抹掉
	//
	// 历史上这两种情况被合并处理 ("两种情况都应该上传本地配置"), 只因为下方的类型
	// 断言恒失败才没有真正酿成事故。现在显式分开, 并把进程标记为"配置不可信"
	if fetchErr != nil {
		m.configTrusted.Store(false)
		log.Printf("[D1Sync] !!! 无法从 D1 拉取配置: %v", fetchErr)
		log.Printf("[D1Sync] !!! 本进程将以【配置只读】模式运行: 使用本地兜底配置对外服务, " +
			"但禁止向 D1 上传配置, 以免用兜底配置覆盖线上。恢复方式: 后台点「从 D1 拉取」, 或在 D1 可达后重启本节点")
		return nil, false, fmt.Errorf("fetch remote config: %w", fetchErr)
	}

	if len(maps) == 0 && len(others) == 0 {
		// 远端确认为空: 播种本地配置作为初始版本
		log.Printf("[D1Sync] Remote config is empty, uploading local config as initial version")

		localConfigAny, loadErr := m.configLoader.LoadConfig()
		if loadErr != nil {
			return nil, false, fmt.Errorf("failed to load local config: %w", loadErr)
		}

		// 本地配置是 *config.Config 而非 map, 过一遍 JSON 转成通用 map。
		// 旧代码在这里直接做 map[string]any 断言, 恒失败, 导致播种从未真正生效
		localConfig, convErr := toGenericMap(localConfigAny)
		if convErr != nil {
			return nil, false, fmt.Errorf("convert local config: %w", convErr)
		}

		// 远端为空是已知状态, 允许上传
		m.configTrusted.Store(true)
		if uploadErr := m.UploadConfig(ctx, localConfigAny); uploadErr != nil {
			log.Printf("[D1Sync] Warning: failed to upload initial config: %v", uploadErr)
		} else {
			log.Printf("[D1Sync] Successfully uploaded local config as initial version")
		}

		return localConfig, true, nil
	}

	// 远程存在有效配置，重建配置对象
	log.Printf("[D1Sync] Remote config found (%d maps, %d others), downloading...", len(maps), len(others))

	config := rebuildConfigData(maps, others)

	// 对齐配置版本位点: others 里已经带着 __config_version, 不必再打一次 D1
	AdvanceConfigCursor(extractConfigVersion(others))

	// 已确认远端状态, 本进程的配置与 D1 对齐, 放开上传
	m.configTrusted.Store(true)

	// 不再保存到本地文件，直接返回配置数据
	log.Printf("[D1Sync] Successfully downloaded remote config (%d maps, %d others)",
		len(maps), len(others))
	return config, false, nil
}

// configVersionKey 存在 config_other 里的配置版本号 (ms), 上传时写入, 心跳按它判断增量。
// 以 __ 开头的 key 是同步层内部状态, 重建配置时一律跳过, 不进业务配置
const configVersionKey = "__config_version"

// extractConfigVersion 从 config_other 行里解析版本号, 缺失或损坏返回 0
func extractConfigVersion(others []ConfigOther) int64 {
	for _, other := range others {
		if other.Key != configVersionKey {
			continue
		}
		var v int64
		if err := json.Unmarshal([]byte(other.Value), &v); err == nil {
			return v
		}
		return 0
	}
	return 0
}

// rebuildConfigData 把列式存储的 maps/others 还原为通用配置 map
//
// 启动下载与心跳热应用共用: 两条路径拿到的配置必须逐字节一致, 否则同一份 D1 数据
// 会因入口不同而行为漂移
func rebuildConfigData(maps []ConfigMap, others []ConfigOther) map[string]any {
	config := make(map[string]any)

	if len(maps) > 0 {
		mapConfig := make(map[string]any)
		for _, cm := range maps {
			pathConfig := map[string]any{
				"DefaultTarget": cm.DefaultTarget,
				"Enabled":       cm.IsEnabled(), // 使用方法转换为 bool
			}

			// 解析 ExtensionRules JSON (应该是数组格式)
			if cm.ExtensionRules != "" {
				var extRules []any
				if err := json.Unmarshal([]byte(cm.ExtensionRules), &extRules); err == nil {
					pathConfig["ExtensionMap"] = extRules
				} else {
					log.Printf("[D1Sync] Warning: failed to parse ExtensionRules for path %s: %v", cm.Path, err)
				}
			}

			// 解析 CacheConfig JSON
			if cm.CacheConfig != "" {
				var cacheConf map[string]any
				if err := json.Unmarshal([]byte(cm.CacheConfig), &cacheConf); err == nil {
					pathConfig["CacheConfig"] = cacheConf
				}
			}

			// 合并 extra_config: 无专属列的字段 (DefaultTargets / RefererBan / RefererRedirect / CFImageOpt / RedirectMode)
			if cm.ExtraConfig != "" {
				var extra map[string]any
				if err := json.Unmarshal([]byte(cm.ExtraConfig), &extra); err == nil {
					for k, v := range extra {
						pathConfig[k] = v
					}
				} else {
					log.Printf("[D1Sync] Warning: failed to parse extra_config for path %s: %v", cm.Path, err)
				}
			}

			mapConfig[cm.Path] = pathConfig
		}
		config["MAP"] = mapConfig
	}

	// 重建其他配置; __ 前缀是同步层内部 key (版本号等), 不属于业务配置
	for _, other := range others {
		if strings.HasPrefix(other.Key, "__") {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(other.Value), &value); err == nil {
			config[other.Key] = value
		}
	}

	return config
}

// sendEvent 发送事件
func (m *D1Manager) sendEvent(event SyncEvent) {
	select {
	case m.eventChan <- event:
	default:
		// 如果通道满了，丢弃最旧的事件
		select {
		case <-m.eventChan:
		default:
		}
		m.eventChan <- event
	}
}
