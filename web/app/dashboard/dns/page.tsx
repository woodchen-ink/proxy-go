"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { PageHeader } from "@/components/page-header";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { CredentialsCard } from "./components/CredentialsCard";
import { DomainCard } from "./components/DomainCard";
import { DomainDialog } from "./components/DomainDialog";
import { LineDialog } from "./components/LineDialog";
import {
  LINES,
  readError,
  type Binding,
  type Credential,
  type Domain,
  type NodeOption,
} from "./types";

type LineEdit = { domain: Domain; binding: Binding | null } | null;

// DnsPage 按域名组织: 域名上配一次账号, 其下 默认 / 境内 / 境外 每条线路一行, 点节点即切换
export default function DnsPage() {
  const { toast } = useToast();
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [domainEdit, setDomainEdit] = useState<{
    open: boolean;
    domain: Domain | null;
  }>({
    open: false,
    domain: null,
  });
  const [lineEdit, setLineEdit] = useState<LineEdit>(null);
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(async () => {
    const [c, d, b, n] = await Promise.all([
      fetchApi("/admin/api/dns/credentials").then((r) => r.json()),
      fetchApi("/admin/api/dns/domains").then((r) => r.json()),
      fetchApi("/admin/api/dns/bindings").then((r) => r.json()),
      fetchApi("/admin/api/hub/nodes").then((r) => r.json()),
    ]);
    setCredentials(c.credentials || []);
    setDomains(d.domains || []);
    setBindings(b.bindings || []);
    setNodes(n.nodes || []);
  }, []);

  useEffect(() => {
    load().catch(() => {});
    const timer = setInterval(() => load().catch(() => {}), 30_000);
    return () => clearInterval(timer);
  }, [load]);

  // 每个域名的线路按 默认 → 境内 → 境外 排
  const linesOf = useMemo(() => {
    const m = new Map<string, Binding[]>();
    for (const b of bindings)
      m.set(b.hostname, [...(m.get(b.hostname) || []), b]);
    const rank = (b: Binding) =>
      LINES.indexOf(b.record_line as (typeof LINES)[number]);
    for (const list of m.values()) list.sort((a, b) => rank(a) - rank(b));
    return m;
  }, [bindings]);

  const run = async (
    id: number,
    fn: () => Promise<Response>,
    onOK?: (data: Record<string, unknown>) => void,
  ) => {
    setBusy(id);
    try {
      const res = await fn();
      if (!res.ok) throw new Error(await readError(res));
      onOK?.(await res.json());
      await load();
    } catch (e) {
      toast({
        title: "操作失败",
        description: e instanceof Error ? e.message : String(e),
        variant: "destructive",
      });
    } finally {
      setBusy(null);
    }
  };

  const post = (url: string, body?: unknown) =>
    fetchApi(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });

  const reportSwitch = (title: string) => (data: Record<string, unknown>) => {
    const errors = (data.errors as string[]) || [];
    const parts = (["cf", "dnspod", "eo"] as const)
      .filter((k) => data[k] && data[k] !== "n/a")
      .map(
        (k) => `${{ cf: "CF", dnspod: "DNSPod", eo: "EdgeOne" }[k]} ${data[k]}`,
      );
    toast({
      title: errors.length ? "部分失败" : title,
      description: [...parts, ...errors].join(" · "),
      variant: errors.length ? "destructive" : undefined,
    });
  };

  const switchTo = (b: Binding, nodeId: string) => {
    const shared =
      b.kind === "cf_dnspod"
        ? (linesOf.get(b.hostname) || []).filter((l) => l.kind === "cf_dnspod")
        : [b];
    const which = shared.map((l) => `「${l.record_line}」`).join("");
    if (
      !confirm(
        `把 ${b.hostname} ${which}线路切到 ${nodeId}? 该节点会成为首选。`,
      )
    )
      return;
    run(
      b.id,
      () => post(`/admin/api/dns/bindings/${b.id}/switch`, { node_id: nodeId }),
      reportSwitch("已切换"),
    );
  };

  const sync = (b: Binding) =>
    run(
      b.id,
      () => post(`/admin/api/dns/bindings/${b.id}/sync`),
      reportSwitch("已同步"),
    );

  const check = (b: Binding) =>
    run(b.id, () => post(`/admin/api/dns/bindings/${b.id}/check`));

  const removeLine = (b: Binding) => {
    if (
      !confirm(
        `删除 ${b.hostname} 的「${b.record_line}」线路? 只删中控里的记录, 不改线上解析。`,
      )
    )
      return;
    run(b.id, () =>
      fetchApi(`/admin/api/dns/bindings/${b.id}`, { method: "DELETE" }),
    );
  };

  const removeDomain = (d: Domain) => {
    if (
      !confirm(
        `删除 ${d.hostname} 及其全部线路? 只删中控里的记录, 不改线上解析。`,
      )
    )
      return;
    run(-d.id, () =>
      fetchApi(`/admin/api/dns/domains/${d.id}`, { method: "DELETE" }),
    );
  };

  return (
    <div className="space-y-5">
      <PageHeader
        title="DNS 分流"
        description="每个域名配一次账号, 默认 / 境内 / 境外 各线路选走法与节点池。高亮的是当前承接流量的节点, 点击其他节点即切换。节点失联 1 分钟且回源探测失败时自动切到下一个健康节点, 首选节点恢复 10 分钟后自动切回, 全程推送企业微信。"
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => load()}>
              <RefreshCw className="size-4" />
              刷新
            </Button>
            <Button
              size="sm"
              onClick={() => setDomainEdit({ open: true, domain: null })}
            >
              <Plus className="size-4" />
              添加域名
            </Button>
          </>
        }
      />

      {domains.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center text-sm text-foreground-muted">
            还没有域名
          </CardContent>
        </Card>
      )}

      {domains.map((d) => (
        <DomainCard
          key={d.id}
          domain={d}
          lines={linesOf.get(d.hostname) || []}
          credentials={credentials}
          nodes={nodes}
          busy={busy}
          onEditDomain={() => setDomainEdit({ open: true, domain: d })}
          onDeleteDomain={() => removeDomain(d)}
          onAddLine={() => setLineEdit({ domain: d, binding: null })}
          onEditLine={(b) => setLineEdit({ domain: d, binding: b })}
          onSwitch={switchTo}
          onSync={sync}
          onCheck={check}
          onDeleteLine={removeLine}
        />
      ))}

      <CredentialsCard credentials={credentials} onChange={() => load()} />

      <DomainDialog
        open={domainEdit.open}
        domain={domainEdit.domain}
        credentials={credentials}
        onClose={() => setDomainEdit((s) => ({ ...s, open: false }))}
        onSaved={() => {
          setDomainEdit((s) => ({ ...s, open: false }));
          load();
        }}
      />
      <LineDialog
        open={!!lineEdit}
        domain={lineEdit?.domain ?? null}
        lines={lineEdit ? linesOf.get(lineEdit.domain.hostname) || [] : []}
        binding={lineEdit?.binding ?? null}
        nodes={nodes}
        onClose={() => setLineEdit(null)}
        onSaved={() => {
          setLineEdit(null);
          load();
        }}
      />
    </div>
  );
}
