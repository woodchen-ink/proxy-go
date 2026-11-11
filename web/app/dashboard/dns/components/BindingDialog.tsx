"use client";

import { useEffect, useState } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import {
  bindingTarget,
  type Binding,
  type BindingKind,
  type Credential,
  type NodeOption,
  readError,
} from "../types";

interface Props {
  open: boolean;
  binding: Binding | null;
  credentials: Credential[];
  nodes: NodeOption[];
  onClose: () => void;
  onSaved: () => void;
}

const EMPTY = {
  kind: "cf_dnspod" as BindingKind,
  hostname: "",
  cf_credential_id: 0,
  saas_zone_id: "",
  saas_zone_name: "",
  dnspod_credential_id: 0,
  dnspod_domain: "",
  record_name: "@",
  record_line: "默认",
  pool: [] as string[],
  auto_failover: true,
  auto_failback: true,
  enabled: true,
  note: "",
};

// BindingDialog 新建 / 编辑 DNS 绑定; 节点池有序, 第一个是首选节点
export function BindingDialog({
  open,
  binding,
  credentials,
  nodes,
  onClose,
  onSaved,
}: Props) {
  const { toast } = useToast();
  const [form, setForm] = useState({ ...EMPTY });
  const [zones, setZones] = useState<{ id: string; name: string }[]>([]);
  const [domains, setDomains] = useState<string[]>([]);
  const [eoZones, setEoZones] = useState<{ id: string; name: string }[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setForm(binding ? { ...EMPTY, ...binding } : { ...EMPTY });
  }, [open, binding]);

  useEffect(() => {
    if (!form.cf_credential_id) return;
    fetchApi(`/admin/api/dns/credentials/${form.cf_credential_id}/discover`)
      .then((r) => r.json())
      .then((d) => setZones(d.zones || []))
      .catch(() => setZones([]));
  }, [form.cf_credential_id]);

  useEffect(() => {
    if (!form.dnspod_credential_id) return;
    fetchApi(`/admin/api/dns/credentials/${form.dnspod_credential_id}/discover`)
      .then((r) => r.json())
      .then((d) => {
        setDomains(d.domains || []);
        setEoZones(d.eo_zones || []);
      })
      .catch(() => {
        setDomains([]);
        setEoZones([]);
      });
  }, [form.dnspod_credential_id]);

  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const togglePool = (id: string) =>
    set(
      "pool",
      form.pool.includes(id)
        ? form.pool.filter((p) => p !== id)
        : [...form.pool, id],
    );

  const move = (idx: number, delta: number) => {
    const next = [...form.pool];
    const j = idx + delta;
    if (j < 0 || j >= next.length) return;
    [next[idx], next[j]] = [next[j], next[idx]];
    set("pool", next);
  };

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetchApi("/admin/api/dns/bindings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...form, id: binding?.id }),
      });
      if (!res.ok) throw new Error(await readError(res));
      toast({
        title: "绑定已保存",
        description: "保存不会改动线上解析, 需要时在列表里手动切换",
      });
      onSaved();
    } catch (e) {
      toast({
        title: "保存失败",
        description: e instanceof Error ? e.message : String(e),
        variant: "destructive",
      });
    } finally {
      setSaving(false);
    }
  };

  const cfCreds = credentials.filter((c) => c.kind === "cloudflare");
  const podCreds = credentials.filter((c) => c.kind === "dnspod");
  const isEO = form.kind === "edgeone";
  const isCF = form.kind === "cf_dnspod";
  const targetOf = (n?: NodeOption) => bindingTarget(form.kind, n);
  const registered = nodes.filter((n) => targetOf(n));

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {binding ? `编辑 ${binding.hostname}` : "新建 DNS 绑定"}
          </DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1 sm:col-span-2">
            <Label>线路类型</Label>
            <Select
              value={form.kind}
              disabled={!!binding}
              onValueChange={(v) =>
                setForm((f) => ({
                  ...f,
                  kind: v as BindingKind,
                  pool: [],
                  saas_zone_id: "",
                  saas_zone_name: "",
                }))
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="cf_dnspod">
                  CF SaaS 回源 + DNSPod 线路记录 (回源主机名)
                </SelectItem>
                <SelectItem value="edgeone">
                  EdgeOne 加速域名源站 (节点 IP)
                </SelectItem>
                <SelectItem value="dnspod_ip">
                  DNSPod 线路记录直接解析到节点 IP (不经 CDN)
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1 sm:col-span-2">
            <Label>访问域名</Label>
            <Input
              value={form.hostname}
              onChange={(e) => set("hostname", e.target.value)}
              placeholder="i.czl.net"
            />
          </div>

          {isCF && (
            <>
              <div className="space-y-1">
                <Label>CF 凭据 (SaaS 区所在账号)</Label>
                <Select
                  value={String(form.cf_credential_id || "")}
                  onValueChange={(v) => set("cf_credential_id", Number(v))}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="选择凭据" />
                  </SelectTrigger>
                  <SelectContent>
                    {cfCreds.map((c) => (
                      <SelectItem key={c.id} value={String(c.id)}>
                        {c.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label>SaaS 区</Label>
                <Select
                  value={form.saas_zone_id}
                  onValueChange={(v) =>
                    setForm((f) => ({
                      ...f,
                      saas_zone_id: v,
                      saas_zone_name: zones.find((z) => z.id === v)?.name || "",
                    }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue
                      placeholder={form.saas_zone_name || "选择 zone"}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {zones.map((z) => (
                      <SelectItem key={z.id} value={z.id}>
                        {z.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </>
          )}

          <div className="space-y-1">
            <Label>{isEO ? "腾讯云凭据" : "DNSPod 凭据"}</Label>
            <Select
              value={String(form.dnspod_credential_id || "")}
              onValueChange={(v) => set("dnspod_credential_id", Number(v))}
            >
              <SelectTrigger>
                <SelectValue placeholder="选择凭据" />
              </SelectTrigger>
              <SelectContent>
                {podCreds.map((c) => (
                  <SelectItem key={c.id} value={String(c.id)}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {isEO ? (
            <div className="space-y-1">
              <Label>EdgeOne 站点</Label>
              <Select
                value={form.saas_zone_id}
                onValueChange={(v) =>
                  setForm((f) => ({
                    ...f,
                    saas_zone_id: v,
                    saas_zone_name: eoZones.find((z) => z.id === v)?.name || "",
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue
                    placeholder={form.saas_zone_name || "选择站点"}
                  />
                </SelectTrigger>
                <SelectContent>
                  {eoZones.map((z) => (
                    <SelectItem key={z.id} value={z.id}>
                      {z.name} ({z.id})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : (
            <>
              <div className="space-y-1">
                <Label>DNSPod 域名</Label>
                <Select
                  value={form.dnspod_domain}
                  onValueChange={(v) => set("dnspod_domain", v)}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="选择域名" />
                  </SelectTrigger>
                  <SelectContent>
                    {(domains.includes(form.dnspod_domain) ||
                    !form.dnspod_domain
                      ? domains
                      : [form.dnspod_domain, ...domains]
                    ).map((d) => (
                      <SelectItem key={d} value={d}>
                        {d}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1">
                <Label>主机记录</Label>
                <Input
                  value={form.record_name}
                  onChange={(e) => set("record_name", e.target.value)}
                  placeholder="@"
                />
              </div>
              <div className="space-y-1">
                <Label>线路</Label>
                <Select
                  value={form.record_line}
                  onValueChange={(v) => set("record_line", v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["默认", "境外", "境内"].map((l) => (
                      <SelectItem key={l} value={l}>
                        {l}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </>
          )}

          <div className="space-y-2 sm:col-span-2">
            <Label>
              节点池 (按优先级排序, 第一个是首选;{" "}
              {isCF ? "只列已登记回源主机名的节点" : "解析到节点出口 IP"})
            </Label>
            <div className="space-y-1 rounded-md border p-2">
              {form.pool.map((id, idx) => (
                <div key={id} className="flex items-center gap-2 text-sm">
                  <span className="w-5 text-muted-foreground">{idx + 1}</span>
                  <span className="flex-1 font-mono text-xs">
                    {id} →{" "}
                    {targetOf(nodes.find((n) => n.node_id === id)) || "未登记"}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    onClick={() => move(idx, -1)}
                    aria-label="上移"
                  >
                    <ArrowUp className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    onClick={() => move(idx, 1)}
                    aria-label="下移"
                  >
                    <ArrowDown className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7"
                    onClick={() => togglePool(id)}
                  >
                    移出
                  </Button>
                </div>
              ))}
              <div className="flex flex-wrap gap-2 pt-1">
                {registered
                  .filter((n) => !form.pool.includes(n.node_id))
                  .map((n) => (
                    <Button
                      key={n.node_id}
                      variant="outline"
                      size="sm"
                      className="h-7 font-mono text-xs"
                      onClick={() => togglePool(n.node_id)}
                    >
                      + {n.node_id}
                    </Button>
                  ))}
              </div>
            </div>
          </div>

          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={form.auto_failover}
              onCheckedChange={(v) => set("auto_failover", v)}
            />
            节点故障自动切换
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={form.auto_failback}
              onCheckedChange={(v) => set("auto_failback", v)}
            />
            首选节点恢复后自动切回
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={form.enabled}
              onCheckedChange={(v) => set("enabled", v)}
            />
            启用
          </label>
          <div className="space-y-1 sm:col-span-2">
            <Label>备注</Label>
            <Input
              value={form.note}
              onChange={(e) => set("note", e.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={saving}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
