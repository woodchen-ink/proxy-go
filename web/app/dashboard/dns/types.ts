// DNS 绑定页的数据契约 (对应 cloudflare-worker/src/dns/binding.ts)

export interface Credential {
  id: number;
  kind: "cloudflare" | "dnspod";
  name: string;
  created_at: number;
  updated_at: number;
}

export interface BindingCheck {
  at: number;
  expected: string;
  dnspod_expected?: string;
  cf_origin: string;
  cf_status: string;
  cf_error: string;
  dnspod_value: string;
  dnspod_record_id: number;
  dnspod_error: string;
  eo_origin: string;
  eo_status: string;
  eo_error: string;
  consistent: boolean;
  actual_node: string;
}

export type BindingKind = "cf_dnspod" | "edgeone" | "dnspod_ip";

// bindingTarget 该线路切到节点时写入的值: CF 线路用回源主机名, EdgeOne / DNSPod 纯 IP 用节点出口 IP
export function bindingTarget(kind: BindingKind, n?: NodeOption): string {
  if (!n) return "";
  return kind === "cf_dnspod" ? n.origin_host : n.ipv4 || n.ipv6;
}

export const LINES = ["默认", "境内", "境外"] as const;

// Method 页面上的「走法」; cf_pref 存储上是 cf_dnspod + dnspod_target (优选域名)
export type Method = "cf" | "cf_pref" | "edgeone" | "dnspod_ip";

export const METHOD_LABEL: Record<Method, string> = {
  cf: "CF 回源",
  cf_pref: "CF 优选 CNAME",
  edgeone: "EdgeOne",
  dnspod_ip: "直连节点 IP",
};

export const METHOD_HINT: Record<Method, string> = {
  cf: "DNSPod 线路 CNAME 到节点回源主机名, 经 CF SaaS 回源",
  cf_pref: "DNSPod 线路固定 CNAME 到优选域名, 切换只改 CF SaaS 回源",
  edgeone: "改 EdgeOne 加速域名的源站为节点出口 IP",
  dnspod_ip: "DNSPod 线路直接解析到节点出口 IP, 不经 CDN",
};

export function methodOf(b: Pick<Binding, "kind" | "dnspod_target">): Method {
  if (b.kind === "cf_dnspod") return b.dnspod_target ? "cf_pref" : "cf";
  return b.kind;
}

export function kindOf(m: Method): BindingKind {
  return m === "cf" || m === "cf_pref" ? "cf_dnspod" : m;
}

// missingFor 域名缺哪些配置导致这种走法用不了; 与中控 hub/dns_domains.ts 同口径
export function missingFor(d: Domain, kind: BindingKind): string {
  if (!d.tc_credential_id) return "域名未配置腾讯云凭据";
  if (kind === "edgeone") return d.eo_zone_id ? "" : "域名未配置 EdgeOne 站点";
  if (!d.dnspod_domain) return "域名未配置 DNSPod 域名";
  if (kind === "cf_dnspod" && (!d.cf_credential_id || !d.saas_zone_id))
    return "域名未配置 CF 凭据或 SaaS 区";
  return "";
}

export interface Domain {
  id: number;
  hostname: string;
  cf_credential_id: number;
  saas_zone_id: string;
  saas_zone_name: string;
  tc_credential_id: number;
  dnspod_domain: string;
  record_name: string;
  eo_zone_id: string;
  eo_zone_name: string;
  note: string;
}

export interface Binding {
  id: number;
  kind: BindingKind;
  hostname: string;
  enabled: boolean;
  cf_credential_id: number;
  saas_zone_id: string;
  saas_zone_name: string;
  dnspod_credential_id: number;
  dnspod_domain: string;
  record_name: string;
  record_line: string;
  dnspod_target: string;
  pool: string[];
  active_node: string;
  auto_failover: boolean;
  auto_failback: boolean;
  last_switch_at: number;
  last_check: BindingCheck | null;
  note: string;
}

export interface NodeOption {
  node_id: string;
  origin_host: string;
  ipv4: string;
  ipv6: string;
  online: boolean;
  state: "up" | "down";
}

export async function readError(res: Response): Promise<string> {
  const data = await res.json().catch(() => ({}));
  return data.error || `HTTP ${res.status}`;
}

// cfLeader 同域名下的普通 CF 回源线路; 优选线路没有自己的节点池, 跟随它的 CF 回源
export function cfLeader(lines: Binding[], excludeId = 0): Binding | undefined {
  return lines.find(
    (l) => l.kind === "cf_dnspod" && !l.dnspod_target && l.id !== excludeId,
  );
}
