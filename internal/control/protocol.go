// Package control 节点 ↔ 中控 (cloudflare-worker Hub DO) 的 WebSocket 通道
//
// 协议定义以 cloudflare-worker/src/hub/protocol.ts 为准, 两边字段名必须一致:
//   - 节点上行 hello / snapshot / ack / rpc_res, 中控下行 welcome / cmd / rpc
//   - 保活是纯文本 ping/pong, 中控用 auto-response 回复, 不唤醒 DO
//   - 指令按 seq 有序, 执行后落盘游标; 重连 hello 带游标, 中控补发之后的指令
package control

import "encoding/json"

type inbound struct {
	T string `json:"t"`
	// welcome
	LatestSeq  int64 `json:"latest_seq"`
	ServerTime int64 `json:"server_time"`
	// cmd
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	// rpc
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type helloMsg struct {
	T        string `json:"t"`
	NodeID   string `json:"node_id"`
	Version  string `json:"version"`
	Cursor   int64  `json:"cursor"`
	IPv4     string `json:"ipv4,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

type ackMsg struct {
	T       string `json:"t"`
	Seq     int64  `json:"seq"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Result  any    `json:"result,omitempty"`
}

type rpcResultMsg struct {
	T     string `json:"t"`
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type snapshotMsg struct {
	T    string    `json:"t"`
	Data *Snapshot `json:"data"`
}
