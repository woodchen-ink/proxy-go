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
import { Switch } from "@/components/ui/switch";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import {
  bindingTarget,
  cfLeader,
  kindOf,
  LINES,
  METHOD_HINT,
  METHOD_LABEL,
  methodOf,
  missingFor,
  readError,
  type Binding,
  type Domain,
  type Method,
  type NodeOption,
} from "../types";
import { OptionSelect } from "./OptionSelect";
import { PoolEditor } from "./PoolEditor";

interface Props {
  open: boolean;
  domain: Domain | null;
  lines: Binding[]; // 该域名已有的线路
  binding: Binding | null; // null = 新建
  nodes: NodeOption[];
  onClose: () => void;
  onSaved: () => void;
}

const EMPTY = {
  record_line: "默认",
  method: "cf" as Method,
  dnspod_target: "",
  pool: [] as string[],
  auto_failover: true,
  auto_failback: true,
  enabled: true,
  note: "",
};

// LineDialog 域名下一条线路: 选线路、走法与节点池; 账号配置来自域名
export function LineDialog({
  open,
  domain,
  lines,
  binding,
  nodes,
  onClose,
  onSaved,
}: Props) {
  const { toast } = useToast();
  const [form, setForm] = useState({ ...EMPTY });
  const [saving, setSaving] = useState(false);
  const others = lines.filter((l) => l.id !== binding?.id);
  const cfSibling = others.find((l) => l.kind === "cf_dnspod");

  // 只在打开或换了编辑对象时初始化: 页面每 30s 轮询会换新的 binding / lines 对象, 跟着它们重置会冲掉正在填的表单
  useEffect(() => {
    if (!open) return;
    if (binding) {
      setForm({ ...EMPTY, ...binding, method: methodOf(binding) });
      return;
    }
    const free = LINES.find((l) => !lines.some((b) => b.record_line === l));
    setForm({ ...EMPTY, record_line: free ?? "默认" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, binding?.id]);

  const kind = kindOf(form.method);
  const shareCF = kind === "cf_dnspod" && !!cfSibling;
  // 优选线路跟随普通 CF 线路时不设节点池与自动切换
  const leader =
    form.method === "cf_pref" ? cfLeader(lines, binding?.id) : undefined;
  // CF 线路共用回源: 新加的 CF 线路沿用已有 CF 线路的节点池
  useEffect(() => {
    if (open && !binding && shareCF && cfSibling)
      setForm((f) => (f.pool.length ? f : { ...f, pool: cfSibling.pool }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, binding?.id, shareCF]);

  if (!domain) return null;

  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));
  const target = (n?: NodeOption) => bindingTarget(kind, n);

  const changeMethod = (m: Method) =>
    setForm((f) => ({
      ...f,
      method: m,
      // 换走法后节点目标值不同 (回源主机名 ↔ 出口 IP), 只保留仍有目标值的节点
      pool: f.pool.filter((id) =>
        bindingTarget(
          kindOf(m),
          nodes.find((n) => n.node_id === id),
        ),
      ),
    }));

  const methodOptions = (Object.keys(METHOD_LABEL) as Method[]).map((m) => {
    const why =
      missingFor(domain, kindOf(m)) ||
      (m === "edgeone" && others.some((l) => l.kind === "edgeone")
        ? "EdgeOne 已用在其他线路"
        : "");
    return {
      value: m,
      label: why ? `${METHOD_LABEL[m]} (${why})` : METHOD_LABEL[m],
      disabled: !!why,
    };
  });

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetchApi("/admin/api/dns/bindings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          id: binding?.id,
          hostname: domain.hostname,
          kind,
          record_line: form.record_line,
          dnspod_target: form.method === "cf_pref" ? form.dnspod_target : "",
          pool: leader ? leader.pool : form.pool,
          active_node: binding?.active_node,
          auto_failover: form.auto_failover,
          auto_failback: form.auto_failback,
          enabled: form.enabled,
          note: form.note,
        }),
      });
      if (!res.ok) throw new Error(await readError(res));
      toast({
        title: "线路已保存",
        description: "保存不会改动线上解析, 需要时点「同步到线上」",
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

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {domain.hostname} ·{" "}
            {binding ? `编辑${binding.record_line}线路` : "添加线路"}
          </DialogTitle>
        </DialogHeader>

        <div className="grid gap-4 sm:grid-cols-2">
          <OptionSelect
            label="线路"
            value={form.record_line}
            options={LINES.map((l) => ({
              value: l,
              label: others.some((o) => o.record_line === l)
                ? `${l} (已配置)`
                : l,
              disabled: others.some((o) => o.record_line === l),
            }))}
            onChange={(v) => set("record_line", v)}
          />
          <OptionSelect
            label="走法"
            value={form.method}
            options={methodOptions}
            onChange={(v) => changeMethod(v as Method)}
          />
          <p className="-mt-2 text-xs text-foreground-muted sm:col-span-2">
            {METHOD_HINT[form.method]}
          </p>

          {form.method === "cf_pref" && (
            <div className="space-y-1 sm:col-span-2">
              <Label>优选域名</Label>
              <Input
                value={form.dnspod_target}
                onChange={(e) => set("dnspod_target", e.target.value)}
                placeholder="cf.czl.net"
              />
            </div>
          )}

          {leader ? (
            <p className="rounded-md border bg-muted/40 px-3 py-2 text-sm text-foreground-muted sm:col-span-2">
              跟随「{leader.record_line}」线路的 CF 回源, 不单独设节点池:
              那条线路切到哪个节点, 访客经优选 IP 进 CF 后就回源到哪个节点 (当前{" "}
              {leader.active_node || "-"})。
            </p>
          ) : (
            <>
              <div className="space-y-2 sm:col-span-2">
                <Label>节点池 (按优先级排序, 第一个是首选)</Label>
                {shareCF && (
                  <p className="text-xs text-foreground-muted">
                    与「{cfSibling!.record_line}」线路共用 CF 回源,
                    节点池、当前节点和自动切换开关会同步, 切换时一起切。
                  </p>
                )}
                <PoolEditor
                  pool={form.pool}
                  nodes={nodes}
                  target={target}
                  onChange={(p) => set("pool", p)}
                />
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
            </>
          )}
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
          <Button
            onClick={save}
            disabled={
              saving ||
              (!leader && form.pool.length === 0) ||
              (form.method === "cf_pref" && !form.dnspod_target.trim())
            }
          >
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
