package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 节点身份解析
//
// node_id 是所有统计表的主键组成部分, 一旦漂移就会在 D1 里产生"僵尸节点":
// 同一台机器的历史流量被当成多个节点重复求和, 且再也合并不回去。
// 因此这里的首要目标不是"取得一个 ID", 而是"取得一个**此后不再变**的 ID"。
//
// 解析顺序 (先命中先用):
//  1. NODE_ID 环境变量  — 显式指定, 永远最高优先级
//  2. data/node_id 文件 — 上一次成功解析的结果, 落在持久卷上
//  3. ip.czl.net 查本机出口 IP 归属 → 生成 ID → **写回文件固化**
//  4. 主机名           — 容器每次重建都会变, 只作应急且**绝不固化**
//  5. "default"
//
// 关键在第 2 步: 只要解析成功过一次, 之后就完全不依赖网络; 即使服务器换了 IP,
// 沿用文件里的旧 ID 才是对的 —— 换 IP 不等于换节点, 统计应当连续。

const (
	// nodeIDLookupTimeout 出口 IP 查询超时; 这一步阻塞启动, 不能久等
	nodeIDLookupTimeout = 5 * time.Second
	// nodeIDLookupURL 无参 GET 即返回调用方自身 IP 与归属
	nodeIDLookupURL = "https://ip.czl.net/api/query"
)

// nodeIDFile 固化位置, 位于 data 持久卷内, 与配置 / 指标同盘 (变量形式便于测试注入)
var nodeIDFile = "data/node_id"

var (
	nodeIDOnce  sync.Once
	nodeIDValue string
)

// NodeID 返回当前节点 ID, 用于多节点上报区分写入键
//
// 全进程只解析一次 (可能涉及网络与磁盘 I/O), 之后返回内存值。
// D1 端按 (path, node_id) 等复合主键存储, 查询时由 worker GROUP BY 跨节点求和
func NodeID() string {
	nodeIDOnce.Do(func() {
		nodeIDValue = resolveNodeID()
		log.Printf("[NodeID] 当前节点 ID: %s", nodeIDValue)
	})
	return nodeIDValue
}

// resolveNodeID 按优先级链解析节点 ID
func resolveNodeID() string {
	if v := sanitizeNodeID(os.Getenv("NODE_ID")); v != "" {
		return v
	}

	if v := readPersistedNodeID(); v != "" {
		log.Printf("[NodeID] 沿用已固化的节点 ID (%s)", nodeIDFile)
		return v
	}

	// 只有接了 D1 才需要区分节点; 单机部署不为此付一次启动期网络请求
	if IsD1ConfigComplete() {
		if v := lookupNodeIDByEgressIP(); v != "" {
			persistNodeID(v)
			return v
		}
	}

	// 兜底不固化: 主机名随容器重建而变, 写进文件反而把漂移永久固定下来,
	// 留着不写, 下次启动仍有机会解析出稳定 ID
	if h, err := os.Hostname(); err == nil && h != "" {
		log.Printf("[NodeID] 未能解析稳定节点 ID, 暂用主机名 %s (不固化); 建议显式配置 NODE_ID", h)
		if v := sanitizeNodeID(h); v != "" {
			return v
		}
	}
	return "default"
}

// egressIPResponse ip.czl.net /api/query 的响应
//
// 按开放式对象处理: 只取本次需要的字段, 服务端新增字段不影响解析
type egressIPResponse struct {
	Code int `json:"code"`
	Data struct {
		IP          string `json:"ip"`
		CountryCode string `json:"countryCode"`
	} `json:"data"`
}

// lookupNodeIDByEgressIP 查本机出口 IP 归属, 生成形如 "de-192.0.2.10" 的节点 ID
//
// 国家码 + 出口 IP 明文: 仪表盘上一眼就能认出是哪台机器, 排查时不用再反查摘要。
// ID 会随统计进 D1 并展示在前端, 等于把出口 IP 暴露给已登录的管理员 —— 自有基础设施,
// 且后台本身就有鉴权, 可读性优先。
//
// IPv6 里的冒号会被归一化成连字符 (见 sanitizeNodeID), 点号保留以维持 IPv4 的可读性。
// 失败返回空串由调用方走兜底, 不阻断启动
func lookupNodeIDByEgressIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), nodeIDLookupTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeIDLookupURL, nil)
	if err != nil {
		log.Printf("[NodeID] 构造出口 IP 查询请求失败: %v", err)
		return ""
	}

	resp, err := (&http.Client{Timeout: nodeIDLookupTimeout}).Do(req)
	if err != nil {
		log.Printf("[NodeID] 查询出口 IP 失败: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[NodeID] 查询出口 IP 返回 HTTP %d", resp.StatusCode)
		return ""
	}

	var result egressIPResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[NodeID] 解析出口 IP 响应失败: %v", err)
		return ""
	}

	// 该服务以业务 code 判定成败, 不能只看 HTTP 状态
	if result.Code != http.StatusOK || result.Data.IP == "" {
		log.Printf("[NodeID] 出口 IP 查询未返回有效结果 (code=%d)", result.Code)
		return ""
	}

	// 国家码缺失时退化为纯 IP, 仍然唯一, 只是少了地区可读性
	prefix := sanitizeNodeID(result.Data.CountryCode)
	if prefix == "" {
		prefix = "node"
	}

	id := sanitizeNodeID(fmt.Sprintf("%s-%s", prefix, result.Data.IP))
	if id == "" {
		return ""
	}
	log.Printf("[NodeID] 由出口 IP 归属解析出节点 ID: %s (%s)", id, result.Data.CountryCode)
	return id
}

// readPersistedNodeID 读取已固化的节点 ID, 不存在或内容非法时返回空串
func readPersistedNodeID() string {
	data, err := os.ReadFile(nodeIDFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[NodeID] 读取 %s 失败: %v", nodeIDFile, err)
		}
		return ""
	}
	return sanitizeNodeID(string(data))
}

// persistNodeID 把解析结果写入持久卷; 失败只记日志, 本次仍用内存中的 ID
func persistNodeID(id string) {
	if err := os.MkdirAll(filepath.Dir(nodeIDFile), 0o755); err != nil {
		log.Printf("[NodeID] 创建 %s 目录失败: %v", filepath.Dir(nodeIDFile), err)
		return
	}
	if err := os.WriteFile(nodeIDFile, []byte(id), 0o644); err != nil {
		log.Printf("[NodeID] 固化节点 ID 到 %s 失败: %v", nodeIDFile, err)
		return
	}
	log.Printf("[NodeID] 已固化节点 ID 到 %s", nodeIDFile)
}

// sanitizeNodeID 归一化为小写 [a-z0-9.-], 其余字符转连字符或丢弃
//
// 点号保留: 节点 ID 里嵌的是出口 IP 明文, "de-192.0.2.10" 比
// "de-192-0-2-10" 可读得多。IPv6 的冒号转连字符 (连续冒号会被下面的
// 去重逻辑收敛), 因为冒号在 URL 与日志里都容易引起歧义。
//
// node_id 会进 SQL 参数与 URL query (两处都已转义), 这里收敛字符集是为了
// 让 ID 在日志 / 前端 / 主键里保持可读一致, 顺便挡住空白与控制字符
func sanitizeNodeID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ':':
			b.WriteRune('-')
		default:
			// 丢弃其余字符 (含空白 / 控制字符 / 非 ASCII)
		}
	}

	// 收敛连续与首尾连字符, 避免出现 "--" (IPv6 的 "::") 或 "-de-" 这类脏值
	id := strings.Trim(b.String(), "-")
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}

	const maxLen = 64
	if len(id) > maxLen {
		id = strings.Trim(id[:maxLen], "-")
	}
	return id
}
