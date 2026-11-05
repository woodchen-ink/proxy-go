"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Globe, Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { BindingDialog } from "./components/BindingDialog";
import { BindingLane } from "./components/BindingLane";
import { CredentialsCard } from "./components/CredentialsCard";
import {
  type Binding,
  type Credential,
  type NodeOption,
  readError,
} from "./types";

// DnsPage 按访问域名分组的分流视图; 每个域名下每条线路 (CF + DNSPod / EdgeOne) 一行
export default function DnsPage() {
  const { toast } = useToast();
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [editing, setEditing] = useState<Binding | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(async () => {
    const [c, b, n] = await Promise.all([
      fetchApi("/admin/api/dns/credentials").then((r) => r.json()),
      fetchApi("/admin/api/dns/bindings").then((r) => r.json()),
      fetchApi("/admin/api/hub/nodes").then((r) => r.json()),
    ]);
    setCredentials(c.credentials || []);
    setBindings(b.bindings || []);
    setNodes(n.nodes || []);
  }, []);

  useEffect(() => {
    load().catch(() => {});
    const timer = setInterval(() => load().catch(() => {}), 30_000);
    return () => clearInterval(timer);
  }, [load]);

  const groups = useMemo(() => {
    const m = new Map<string, Binding[]>();
    for (const b of bindings)
      m.set(b.hostname, [...(m.get(b.hostname) || []), b]);
    // 默认线路 (CF + DNSPod) 排在 EdgeOne 前
    for (const list of m.values())
      list.sort((a, b) =>
        a.kind === b.kind ? 0 : a.kind === "edgeone" ? 1 : -1,
      );
    return [...m.entries()];
  }, [bindings]);

  const run = async (
    b: Binding,
    fn: () => Promise<Response>,
    onOK?: (data: Record<string, unknown>) => void,
  ) => {
    setBusy(b.id);
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

  const check = (b: Binding) =>
    run(b, () =>
      fetchApi(`/admin/api/dns/bindings/${b.id}/check`, { method: "POST" }),
    );

  const switchTo = (b: Binding, nodeId: string) => {
    const n = nodes.find((x) => x.node_id === nodeId);
    const msg =
      b.kind === "edgeone"
        ? `把 ${b.hostname} 的 EdgeOne 源站切到 ${nodeId} (${n?.ipv4 || n?.ipv6})?\n该节点会成为首选。`
        : `把 ${b.hostname} 切到 ${nodeId} (${n?.origin_host})?\n会同时修改 CF SaaS 回源与 DNSPod「${b.record_line}」线路记录, 该节点会成为首选。`;
    if (!confirm(msg)) return;
    run(
      b,
      () =>
        fetchApi(`/admin/api/dns/bindings/${b.id}/switch`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ node_id: nodeId }),
        }),
      (data) => {
        const errors = (data.errors as string[]) || [];
        toast({
          title: errors.length ? "部分失败" : "已切换",
          description: `${b.kind === "edgeone" ? `EdgeOne ${data.eo}` : `CF ${data.cf} · DNSPod ${data.dnspod}`}${errors.length ? ` · ${errors.join("; ")}` : ""}`,
          variant: errors.length ? "destructive" : undefined,
        });
      },
    );
  };

  const remove = (b: Binding) => {
    if (
      !confirm(
        `删除 ${b.hostname} 的这条绑定? 只删中控里的记录, 不改线上解析。`,
      )
    )
      return;
    run(b, () =>
      fetchApi(`/admin/api/dns/bindings/${b.id}`, { method: "DELETE" }),
    );
  };

  const openDialog = (b: Binding | null) => {
    setEditing(b);
    setDialogOpen(true);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">DNS 分流</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
            每条线路按优先级排列节点, 高亮的是当前承接流量的节点;
            点击其他节点即可手动切换。节点故障时自动切到下一个健康节点,
            首选节点恢复 10 分钟后自动切回, 全程推送企业微信。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => load()}>
            <RefreshCw className="mr-2 size-4" />
            刷新
          </Button>
          <Button size="sm" onClick={() => openDialog(null)}>
            <Plus className="mr-2 size-4" />
            新建绑定
          </Button>
        </div>
      </div>

      {groups.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center text-sm text-muted-foreground">
            还没有绑定
          </CardContent>
        </Card>
      )}

      {groups.map(([hostname, list]) => (
        <Card key={hostname}>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-lg">
              <Globe className="size-5 text-muted-foreground" />
              {hostname}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {list.map((b) => (
              <BindingLane
                key={b.id}
                binding={b}
                nodes={nodes}
                busy={busy === b.id}
                onSwitch={switchTo}
                onCheck={check}
                onEdit={openDialog}
                onDelete={remove}
              />
            ))}
          </CardContent>
        </Card>
      ))}

      <CredentialsCard credentials={credentials} onChange={() => load()} />

      <BindingDialog
        open={dialogOpen}
        binding={editing}
        credentials={credentials}
        nodes={nodes}
        onClose={() => setDialogOpen(false)}
        onSaved={() => {
          setDialogOpen(false);
          load();
        }}
      />
    </div>
  );
}
