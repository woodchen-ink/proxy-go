"use client";

import { useEffect, useState } from "react";
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
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { type Credential, type Domain, readError } from "../types";
import { OptionSelect } from "./OptionSelect";

interface Props {
  open: boolean;
  domain: Domain | null; // null = 新建
  credentials: Credential[];
  onClose: () => void;
  onSaved: () => void;
}

type Zone = { id: string; name: string };

const EMPTY = {
  hostname: "",
  cf_credential_id: 0,
  saas_zone_id: "",
  saas_zone_name: "",
  tc_credential_id: 0,
  dnspod_domain: "",
  record_name: "@",
  eo_zone_id: "",
  eo_zone_name: "",
  note: "",
};

// discover 列出凭据下的 CF zone / DNSPod 域名 / EdgeOne 站点
function useDiscover(credentialId: number) {
  const [data, setData] = useState<{
    zones: Zone[];
    domains: string[];
    eo_zones: Zone[];
  }>({ zones: [], domains: [], eo_zones: [] });
  useEffect(() => {
    if (!credentialId) return;
    fetchApi(`/admin/api/dns/credentials/${credentialId}/discover`)
      .then((r) => r.json())
      .then((d) =>
        setData({
          zones: d.zones || [],
          domains: d.domains || [],
          eo_zones: d.eo_zones || [],
        }),
      )
      .catch(() => setData({ zones: [], domains: [], eo_zones: [] }));
  }, [credentialId]);
  return data;
}

// DomainDialog 域名的账号配置: CF 与腾讯云各填一次, 其下各线路共用
export function DomainDialog({
  open,
  domain,
  credentials,
  onClose,
  onSaved,
}: Props) {
  const { toast } = useToast();
  const [form, setForm] = useState({ ...EMPTY });
  const [saving, setSaving] = useState(false);
  const cf = useDiscover(form.cf_credential_id);
  const tc = useDiscover(form.tc_credential_id);

  useEffect(() => {
    if (open) setForm(domain ? { ...EMPTY, ...domain } : { ...EMPTY });
  }, [open, domain]);

  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetchApi("/admin/api/dns/domains", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...form, id: domain?.id }),
      });
      if (!res.ok) throw new Error(await readError(res));
      toast({ title: "域名已保存", description: "保存不会改动线上解析" });
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

  const credOptions = (kind: Credential["kind"]) =>
    credentials
      .filter((c) => c.kind === kind)
      .map((c) => ({ value: String(c.id), label: c.name }));
  const zoneOptions = (zones: Zone[], current: string, name: string) => {
    const opts = zones.map((z) => ({ value: z.id, label: z.name }));
    if (current && !zones.some((z) => z.id === current))
      opts.unshift({ value: current, label: name || current });
    return opts;
  };
  const domainOptions = [
    ...(form.dnspod_domain && !tc.domains.includes(form.dnspod_domain)
      ? [form.dnspod_domain]
      : []),
    ...tc.domains,
  ].map((d) => ({ value: d, label: d }));

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {domain ? `编辑 ${domain.hostname}` : "添加域名"}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-5">
          <div className="space-y-1">
            <Label>访问域名</Label>
            <Input
              value={form.hostname}
              onChange={(e) => set("hostname", e.target.value)}
              placeholder="i.czl.net"
            />
          </div>

          <section className="space-y-3">
            <h3 className="text-sm font-medium">
              Cloudflare{" "}
              <span className="font-normal text-foreground-muted">
                CF 回源 / CF 优选线路用
              </span>
            </h3>
            <div className="grid gap-3 sm:grid-cols-2">
              <OptionSelect
                label="CF 凭据"
                value={String(form.cf_credential_id || "")}
                options={credOptions("cloudflare")}
                placeholder="不使用"
                clearable
                onChange={(v) =>
                  setForm((f) => ({
                    ...f,
                    cf_credential_id: Number(v) || 0,
                    saas_zone_id: "",
                    saas_zone_name: "",
                  }))
                }
              />
              <OptionSelect
                label="SaaS 区"
                value={form.saas_zone_id}
                options={zoneOptions(
                  cf.zones,
                  form.saas_zone_id,
                  form.saas_zone_name,
                )}
                placeholder="选择 zone"
                disabled={!form.cf_credential_id}
                onChange={(v) =>
                  setForm((f) => ({
                    ...f,
                    saas_zone_id: v,
                    saas_zone_name:
                      cf.zones.find((z) => z.id === v)?.name || "",
                  }))
                }
              />
            </div>
          </section>

          <section className="space-y-3">
            <h3 className="text-sm font-medium">
              腾讯云{" "}
              <span className="font-normal text-foreground-muted">
                DNSPod 线路记录与 EdgeOne 共用
              </span>
            </h3>
            <div className="grid gap-3 sm:grid-cols-2">
              <OptionSelect
                label="腾讯云凭据"
                value={String(form.tc_credential_id || "")}
                options={credOptions("dnspod")}
                placeholder="不使用"
                clearable
                onChange={(v) =>
                  setForm((f) => ({
                    ...f,
                    tc_credential_id: Number(v) || 0,
                    dnspod_domain: "",
                    eo_zone_id: "",
                    eo_zone_name: "",
                  }))
                }
              />
              <OptionSelect
                label="EdgeOne 站点"
                value={form.eo_zone_id}
                options={zoneOptions(
                  tc.eo_zones,
                  form.eo_zone_id,
                  form.eo_zone_name,
                )}
                placeholder="不使用"
                clearable
                disabled={!form.tc_credential_id}
                onChange={(v) =>
                  setForm((f) => ({
                    ...f,
                    eo_zone_id: v,
                    eo_zone_name:
                      tc.eo_zones.find((z) => z.id === v)?.name || "",
                  }))
                }
              />
              <OptionSelect
                label="DNSPod 域名"
                value={form.dnspod_domain}
                options={domainOptions}
                placeholder="选择域名"
                disabled={!form.tc_credential_id}
                onChange={(v) => set("dnspod_domain", v)}
              />
              <div className="space-y-1">
                <Label>主机记录</Label>
                <Input
                  value={form.record_name}
                  onChange={(e) => set("record_name", e.target.value)}
                  placeholder="@"
                  disabled={!form.tc_credential_id}
                />
              </div>
            </div>
          </section>

          <div className="space-y-1">
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
          <Button onClick={save} disabled={saving || !form.hostname.trim()}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
