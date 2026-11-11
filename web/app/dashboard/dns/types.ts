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

// bindingTarget 该线路切到节点时写入的值: CF + DNSPod 用回源主机名, EdgeOne / DNSPod 纯 IP 用节点出口 IP
export function bindingTarget(kind: BindingKind, n?: NodeOption): string {
  if (!n) return "";
  return kind === "cf_dnspod" ? n.origin_host : n.ipv4 || n.ipv6;
}

// bindingTitle 线路标题, 如「境内线路 · DNSPod 纯 IP」
export function bindingTitle(b: Pick<Binding, "kind" | "record_line">): string {
  if (b.kind === "edgeone") return "境内线路 · EdgeOne";
  if (b.kind === "dnspod_ip") return `${b.record_line}线路 · DNSPod 纯 IP`;
  return `${b.record_line}线路 · CF SaaS + DNSPod`;
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
