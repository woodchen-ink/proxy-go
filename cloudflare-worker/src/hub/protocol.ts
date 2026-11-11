/**
 * 节点 ↔ 中控 WebSocket 协议
 *
 * 全部是 JSON 文本帧, 用 `t` 区分类型。保活帧例外: 节点每 20s 发纯文本 `ping`,
 * DO 用 setWebSocketAutoResponse 回 `pong`, 不唤醒 DO 也不计费。
 *
 * 指令 (cmd) 有序号 seq, 节点执行后回 ack 并把 seq 落盘为游标; 重连时 hello 带上游标,
 * 中控补发游标之后的指令。游标为 0 (新节点 / 丢文件) 只快进不重放, 避免把历史上的
 * "清空统计"类指令误施加到新节点上。
 */

// ---------- 节点 → 中控 ----------

export interface HelloMsg {
	t: 'hello';
	node_id: string;
	version: string;
	cursor: number;
	ipv4?: string;
	ipv6?: string;
	hostname?: string;
}

export interface SnapshotMsg {
	t: 'snapshot';
	data: NodeSnapshot;
}

export interface AckMsg {
	t: 'ack';
	seq: number;
	ok: boolean;
	message?: string;
	result?: unknown;
}

export interface RpcResultMsg {
	t: 'rpc_res';
	id: string;
	ok: boolean;
	data?: unknown;
	error?: string;
}

export type NodeMessage = HelloMsg | SnapshotMsg | AckMsg | RpcResultMsg;

// ---------- 中控 → 节点 ----------

export interface CommandFrame {
	t: 'cmd';
	seq: number;
	type: CommandType;
	payload: Record<string, unknown>;
}

export interface RpcFrame {
	t: 'rpc';
	id: string;
	method: RpcMethod;
	params: Record<string, unknown>;
}

export interface WelcomeFrame {
	t: 'welcome';
	latest_seq: number;
	server_time: number;
}

/**
 * 指令类型 (中控广播, 每个节点各执行一次):
 *   cache_clear   { type: proxy|mirror|all, scope: all|prefix|urls, prefix?, urls? }
 *   cache_enable  { type: proxy|mirror, enabled }
 *   pulse         立即执行一次心跳 —— 配置热应用 / 重置统计复用心跳通道既有逻辑
 *   unban         { ip }
 */
export type CommandType = 'cache_clear' | 'cache_enable' | 'pulse' | 'unban';

// RPC: 现场向在线节点查询本地数据, 等齐或超时后汇总
//   upgrade  { version, build_time, assets: {arch: {sha256, size}} } 只发给单个节点 (滚动升级),
//            节点下载校验后回结果再 exec 新程序; 不进指令日志, 免得重连补发旧版本
export type RpcMethod = 'security_banned' | 'security_stats' | 'security_check' | 'security_history' | 'upgrade';

// ---------- 快照 ----------

export interface CacheStats {
	total_items: number;
	total_size: number;
	disk_file_count: number;
	disk_size: number;
	hit_count: number;
	miss_count: number;
	hit_rate: number;
	bytes_saved: number;
	enabled: boolean;
	format_fallback_hit: number;
	image_cache_hit: number;
	regular_cache_hit: number;
}

// RawPathStat 节点内存里的逐路径原始累计 (含 total_latency_ns, 派生值由中控重算)
export interface RawPathStat {
	path: string;
	request_count: number;
	error_count: number;
	bytes_transferred: number;
	total_latency_ns: number;
	last_access_time: number;
	status_2xx: number;
	status_3xx: number;
	status_4xx: number;
	status_5xx: number;
	cache_hits: number;
	cache_misses: number;
	bytes_saved: number;
}

export interface RequestLog {
	Time: string;
	Host?: string; // 旧版节点不带
	Path: string;
	Status: number;
	Latency: number;
	BytesSent: number;
	ClientIP: string;
	[key: string]: unknown;
}

// NodeSeriesPoint 节点整体的一个时间桶 (原始量), 与 Go metrics.NodeSeriesPoint 对齐
export interface NodeSeriesPoint {
	ts: number; // 桶起点 unix 秒
	requests: number;
	bytes: number;
	origin_bytes: number;
	errors: number;
	cache_hits: number;
	cache_misses: number;
	latency_ns: number;
	big_bytes: number;
	big_ns: number;
	big_count: number;
	speed: number[]; // <1 / 1-5 / 5-20 / 20-50 / >=50 MB/s 各档请求数
}

export interface NodeSnapshot {
	at: number; // 节点生成快照的 unix 毫秒
	uptime_sec: number;
	active_requests: number;
	requests_per_second: number;
	current_bandwidth_bps: number;
	bandwidth_history: Record<string, number>; // HH:MM -> bytes/s
	num_goroutine: number;
	memory_bytes: number;
	latency_sum_ns: number;
	min_latency_ns: number;
	max_latency_ns: number;
	total_bytes: number;
	session_requests: number;
	status_codes: Record<string, number>;
	latency_distribution: Record<string, number>;
	path_stats: RawPathStat[];
	top_referers: unknown[];
	recent_requests: RequestLog[];
	minute_series?: NodeSeriesPoint[]; // 最近 60 分钟的分钟桶; 旧版节点没有
	cache: { proxy?: CacheStats; mirror?: CacheStats };
	config_version: number;
}
