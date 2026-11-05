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

export type BindingKind = "cf_dnspod" | "edgeone";

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
