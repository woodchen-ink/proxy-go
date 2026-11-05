package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/security"
	"proxy-go/internal/service"
	"proxy-go/internal/upgrade"
	d1sync "proxy-go/pkg/sync"
)

// Version 由构建注入 (-ldflags "-X proxy-go/internal/control.Version=v1.2.3"), 上报给中控展示
var Version = "dev"

// BuildTime 构建对应提交的 unix 秒 (-ldflags "-X proxy-go/internal/control.BuildTime=..."),
// 自更新据此区分镜像程序与 data/bin 程序; 本地构建为空
var BuildTime = ""

// BuildTimeUnix BuildTime 解析为整数, 未注入或格式不对返回 0
func BuildTimeUnix() int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(BuildTime), 10, 64)
	return n
}

const (
	cursorFile       = "data/control_cursor"
	snapshotInterval = 15 * time.Second
	pingInterval     = 20 * time.Second
	readTimeout      = 70 * time.Second
	maxBackoff       = 60 * time.Second
)

// Deps 指令执行所需的本地组件
type Deps struct {
	ProxyCache  *cache.CacheManager
	MirrorCache *cache.CacheManager
	BanManager  *security.IPBanManager // IPBan 未启用时为 nil
}

// Client 与中控保持一条长连接; 断线指数退避重连, 进程内单例
type Client struct {
	wsURL    string
	baseURL  string
	token    string
	nodeID   string
	cache    *service.CacheService
	bans     *security.IPBanManager
	cursor   atomic.Int64
	cmdQueue chan inbound
	ipv4     string
	ipv6     string

	writeMu sync.Mutex
	conn    *websocket.Conn
}

var global atomic.Pointer[Client]

// HubBaseURL 中控的 https 根地址 (公开清缓存接口转发用); 未启用时为空
func HubBaseURL() string {
	if c := global.Load(); c != nil {
		return c.baseURL
	}
	return ""
}

// Start 读取环境变量启动通道; 未配置中控地址或令牌时静默不启用 (单机部署)
//
// 中控地址优先 CONTROL_URL, 否则沿用 D1_SYNC_URL (同一个 Worker); 令牌复用 D1_SYNC_TOKEN。
func Start(deps Deps) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("CONTROL_URL")), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("D1_SYNC_URL")), "/")
	}
	token := strings.TrimSpace(os.Getenv("D1_SYNC_TOKEN"))
	if base == "" || token == "" {
		log.Printf("[Control] 未配置中控地址或令牌, 不连接中控")
		return
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		log.Printf("[Control] 中控地址无效: %q", base)
		return
	}
	ws := *u
	ws.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	ws.Path = "/node/ws"

	c := &Client{
		wsURL:    ws.String(),
		baseURL:  base,
		token:    token,
		nodeID:   d1sync.NodeID(),
		cache:    service.NewCacheService(deps.ProxyCache, deps.MirrorCache),
		bans:     deps.BanManager,
		cmdQueue: make(chan inbound, 1024),
	}
	c.cursor.Store(readCursor())
	global.Store(c)

	// 缓存参数随配置下发: 中控改 Cache / MirrorCache 后经心跳热应用到这里
	config.RegisterUpdateCallback(func(cfg *config.Config) {
		if err := c.cache.UpdateCacheConfig("proxy", cfg.Cache); err != nil {
			log.Printf("[Control] 应用 proxy 缓存配置失败: %v", err)
		}
		if err := c.cache.UpdateCacheConfig("mirror", cfg.MirrorCache); err != nil {
			log.Printf("[Control] 应用 mirror 缓存配置失败: %v", err)
		}
	})

	go c.runCommands()
	go c.loop()
	log.Printf("[Control] 中控通道已启动: %s (node=%s)", c.wsURL, c.nodeID)
}

func (c *Client) loop() {
	c.ipv4, c.ipv6 = lookupEgressIP()
	backoff := time.Second
	for {
		started := time.Now()
		err := c.session()
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		log.Printf("[Control] 连接中断: %v, %v 后重连", err, backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}
}

// session 一次完整连接的生命周期; 返回即代表连接已断
func (c *Client) session() error {
	cfg, err := websocket.NewConfig(c.wsURL, c.baseURL)
	if err != nil {
		return err
	}
	cfg.Header = http.Header{
		"Authorization": {"Bearer " + c.token},
		"X-Node-Id":     {c.nodeID},
	}
	cfg.Dialer = nil
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	c.writeMu.Lock()
	c.conn = conn
	c.writeMu.Unlock()
	defer func() {
		c.writeMu.Lock()
		c.conn = nil
		c.writeMu.Unlock()
	}()

	host, _ := os.Hostname()
	if err := c.send(helloMsg{T: "hello", NodeID: c.nodeID, Version: Version, Cursor: c.cursor.Load(), IPv4: c.ipv4, IPv6: c.ipv6, Hostname: host}); err != nil {
		return err
	}
	log.Printf("[Control] 已连上中控 (cursor=%d)", c.cursor.Load())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.heartbeat(ctx, conn)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		var raw string
		if err := websocket.Message.Receive(conn, &raw); err != nil {
			return err
		}
		if raw == "pong" {
			continue
		}
		var msg inbound
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			log.Printf("[Control] 无法解析中控消息: %v", err)
			continue
		}
		switch msg.T {
		case "welcome":
			// 自更新程序首次连上中控即视为可用, 之后容器重启不再按崩溃计数回退
			upgrade.MarkHealthy()
			// 游标为 0 说明是新节点或丢了游标文件: 只快进不重放, 中控也不会补发。
			// 中控序号比游标还小说明中控指令日志被重建过: 跟随中控, 否则之后的新指令都会被当成"已执行"跳过
			if cur := c.cursor.Load(); (cur == 0 && msg.LatestSeq > 0) || msg.LatestSeq < cur {
				c.setCursor(msg.LatestSeq)
			}
		case "cmd":
			select {
			case c.cmdQueue <- msg:
			default:
				// 不能丢: 丢了后面的指令照常执行并推进游标, 这条就永远补不回来。断线让中控按游标重发
				log.Printf("[Control] 指令队列已满 (seq=%d), 断开重连以按游标补发", msg.Seq)
				return fmt.Errorf("command queue full")
			}
		case "rpc":
			go c.handleRPC(msg)
		}
	}
}

// heartbeat 定时发 ping 与实时快照; 连接断开时随 ctx 退出
func (c *Client) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ping := time.NewTicker(pingInterval)
	snap := time.NewTicker(snapshotInterval)
	defer ping.Stop()
	defer snap.Stop()
	c.pushSnapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if err := c.sendText("ping"); err != nil {
				_ = conn.Close()
				return
			}
		case <-snap.C:
			c.pushSnapshot()
		}
	}
}

func (c *Client) pushSnapshot() {
	collector := metrics.GetCollector()
	if collector == nil {
		return
	}
	snap := &Snapshot{LiveStats: collector.LiveSnapshot(), At: time.Now().UnixMilli()}
	stats := c.cache.GetCacheStats()
	snap.Cache = map[string]cache.CacheStats{"proxy": stats["proxy"], "mirror": stats["mirror"]}
	snap.ConfigVersion = d1sync.ConfigCursor()
	if err := c.send(snapshotMsg{T: "snapshot", Data: snap}); err != nil {
		log.Printf("[Control] 推送快照失败: %v", err)
	}
}

func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.sendText(string(b))
}

func (c *Client) sendText(s string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return websocket.Message.Send(c.conn, s)
}

func (c *Client) setCursor(seq int64) {
	c.cursor.Store(seq)
	if err := os.MkdirAll(filepath.Dir(cursorFile), 0o755); err == nil {
		tmp := cursorFile + ".tmp"
		if err := os.WriteFile(tmp, []byte(strconv.FormatInt(seq, 10)), 0o644); err == nil {
			_ = os.Rename(tmp, cursorFile)
		}
	}
}

func readCursor() int64 {
	b, err := os.ReadFile(cursorFile)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

// Snapshot 推给中控的实时快照
type Snapshot struct {
	metrics.LiveStats
	At            int64                       `json:"at"`
	Cache         map[string]cache.CacheStats `json:"cache"`
	ConfigVersion int64                       `json:"config_version"`
}

// lookupEgressIP 查本机出口 IP 供中控匹配回源主机名; 失败返回空, 不影响连接
func lookupEgressIP() (ipv4, ipv6 string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ip.czl.net/api/query", nil)
	if err != nil {
		return "", ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			IP string `json:"ip"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || body.Data.IP == "" {
		return "", ""
	}
	if strings.Contains(body.Data.IP, ":") {
		return "", body.Data.IP
	}
	return body.Data.IP, ""
}
