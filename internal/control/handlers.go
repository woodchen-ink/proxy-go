package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"proxy-go/internal/metrics"
	"proxy-go/internal/upgrade"
)

// runCommands 串行执行指令 (清缓存 / pulse 等可能耗时, 不阻塞读循环与保活)
func (c *Client) runCommands() {
	for msg := range c.cmdQueue {
		// 已执行过的 (重连补发与在途指令重叠) 只补 ack 不重做
		if msg.Seq <= c.cursor.Load() {
			_ = c.send(ackMsg{T: "ack", Seq: msg.Seq, OK: true, Message: "already applied"})
			continue
		}
		start := time.Now()
		result, err := c.execute(msg.Type, msg.Payload)
		ack := ackMsg{T: "ack", Seq: msg.Seq, OK: err == nil, Result: result}
		if err != nil {
			ack.Message = err.Error()
		}
		log.Printf("[Control] 执行指令 seq=%d type=%s ok=%v 耗时=%v", msg.Seq, msg.Type, err == nil, time.Since(start))
		c.setCursor(msg.Seq)
		if sendErr := c.send(ack); sendErr != nil {
			log.Printf("[Control] 回 ack 失败 seq=%d: %v", msg.Seq, sendErr)
		}
	}
}

type cacheClearPayload struct {
	Type   string   `json:"type"`
	Scope  string   `json:"scope"`
	Prefix string   `json:"prefix"`
	URLs   []string `json:"urls"`
	URL    string   `json:"url"`
}

func (c *Client) execute(typ string, raw json.RawMessage) (any, error) {
	switch typ {
	case "cache_clear":
		var p cacheClearPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Type == "" {
			p.Type = "all"
		}
		var n int
		var err error
		switch p.Scope {
		case "all":
			err = c.cache.ClearCache(p.Type)
		case "prefix":
			n, err = c.cache.ClearCacheByPath(p.Type, p.Prefix)
		case "urls":
			n, err = c.cache.ClearCacheByURLs(p.Type, p.URLs)
		case "url":
			n, err = c.cache.ClearCacheByURL(p.Type, p.URL)
		default:
			return nil, fmt.Errorf("unknown scope %q", p.Scope)
		}
		return map[string]int{"cleared_items": n}, err

	case "cache_enable":
		var p struct {
			Type    string `json:"type"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return nil, c.cache.SetCacheEnabled(p.Type, p.Enabled)

	case "pulse":
		// 配置热应用与重置统计都复用心跳下行的既有逻辑, 这里只是把下一次心跳提前
		return nil, metrics.Pulse()

	case "unban":
		var p struct {
			IP string `json:"ip"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if c.bans == nil {
			return map[string]bool{"unbanned": false}, nil
		}
		return map[string]bool{"unbanned": c.bans.UnbanIP(p.IP)}, nil

	default:
		return nil, fmt.Errorf("unknown command %q", typ)
	}
}

const timeLayout = "2006-01-02 15:04:05"

func (c *Client) handleRPC(msg inbound) {
	data, err := c.rpc(msg.Method, msg.Params)
	res := rpcResultMsg{T: "rpc_res", ID: msg.ID, OK: err == nil, Data: data}
	if err != nil {
		res.Error = err.Error()
	}
	if sendErr := c.send(res); sendErr != nil {
		log.Printf("[Control] 回 RPC 结果失败 %s: %v", msg.Method, sendErr)
	}
	// 新程序已就绪: 先把结果回给中控, 再停服 exec (进程被替换, 之后的代码不会执行)
	if msg.Method == "upgrade" && err == nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := upgrade.Restart(); err != nil {
				log.Printf("[Control] 切换新程序失败: %v", err)
			}
		}()
	}
}

func (c *Client) rpc(method string, raw json.RawMessage) (any, error) {
	switch method {
	case "security_banned":
		out := []map[string]any{}
		if c.bans != nil {
			for ip, end := range c.bans.GetBannedIPs() {
				out = append(out, map[string]any{
					"ip":                ip,
					"ban_end_time":      end.Format(timeLayout),
					"remaining_seconds": int64(time.Until(end).Seconds()),
				})
			}
		}
		return map[string]any{"banned_ips": out, "count": len(out)}, nil

	case "security_stats":
		if c.bans == nil {
			return map[string]any{
				"banned_ips_count": 0, "error_records_count": 0, "enabled": false,
				"config": map[string]int{"ErrorThreshold": 0, "WindowMinutes": 0, "BanDurationMinutes": 0, "CleanupIntervalMinutes": 0},
			}, nil
		}
		stats := c.bans.GetStats()
		stats["enabled"] = true
		return stats, nil

	case "security_check":
		var p struct {
			IP string `json:"ip"`
		}
		_ = json.Unmarshal(raw, &p)
		res := map[string]any{"ip": p.IP, "banned": false}
		if c.bans != nil && p.IP != "" {
			if banned, end := c.bans.GetBanInfo(p.IP); banned {
				res["banned"] = true
				res["ban_end_time"] = end.Format(timeLayout)
				res["remaining_seconds"] = int64(time.Until(end).Seconds())
			}
		}
		return res, nil

	case "security_history":
		var p struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Limit <= 0 {
			p.Limit = 100
		}
		items := []map[string]any{}
		if c.bans != nil {
			records, err := c.bans.GetBanHistory(p.Limit)
			if err != nil {
				return nil, err
			}
			for _, r := range records {
				item := map[string]any{
					"ip": r.IP, "ban_time": r.BanTime.Format(timeLayout), "ban_end_time": r.BanEndTime.Format(timeLayout),
					"reason": r.Reason, "error_count": r.ErrorCount, "is_active": r.IsActive,
				}
				if !r.UnbanTime.IsZero() {
					item["unban_time"] = r.UnbanTime.Format(timeLayout)
				}
				if r.UnbanReason != "" {
					item["unban_reason"] = r.UnbanReason
				}
				items = append(items, item)
			}
		}
		return map[string]any{"history": items, "total": len(items)}, nil

	case "upgrade":
		// 滚动升级不走指令日志: 指令会在重连时补发, 7 天后重放一次旧版本升级是灾难
		var req upgrade.Request
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		if req.Version == Version {
			return nil, fmt.Errorf("already running %s", Version)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if err := upgrade.Apply(ctx, req, c.baseURL, c.token, BuildTimeUnix()); err != nil {
			return nil, err
		}
		return map[string]string{"from": Version, "to": req.Version}, nil

	default:
		return nil, fmt.Errorf("unknown rpc %q", method)
	}
}
