"use client";

import { Globe, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  LINES,
  type Binding,
  type Credential,
  type Domain,
  type NodeOption,
} from "../types";
import { BindingLane } from "./BindingLane";

interface Props {
  domain: Domain;
  lines: Binding[];
  credentials: Credential[];
  nodes: NodeOption[];
  busy: number | null;
  onEditDomain: () => void;
  onDeleteDomain: () => void;
  onAddLine: () => void;
  onEditLine: (b: Binding) => void;
  onSwitch: (b: Binding, nodeId: string) => void;
  onSync: (b: Binding) => void;
  onCheck: (b: Binding) => void;
  onDeleteLine: (b: Binding) => void;
}

// DomainCard 一个域名: 标题行带账号摘要与操作, 下面每条线路一行
export function DomainCard({
  domain: d,
  lines,
  credentials,
  nodes,
  busy,
  onEditDomain,
  onDeleteDomain,
  onAddLine,
  onEditLine,
  onSwitch,
  onSync,
  onCheck,
  onDeleteLine,
}: Props) {
  const credName = (id: number) =>
    credentials.find((c) => c.id === id)?.name || `#${id}`;
  const summary = [
    d.cf_credential_id &&
      `CF ${credName(d.cf_credential_id)} / ${d.saas_zone_name || d.saas_zone_id || "未选 SaaS 区"}`,
    d.tc_credential_id &&
      `腾讯云 ${credName(d.tc_credential_id)}${d.dnspod_domain ? ` / DNSPod ${d.record_name === "@" ? "" : `${d.record_name}.`}${d.dnspod_domain}` : ""}`,
    d.eo_zone_id && `EdgeOne ${d.eo_zone_name || d.eo_zone_id}`,
  ].filter(Boolean) as string[];
  const full = lines.length >= LINES.length;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <CardTitle className="flex items-center gap-2 text-lg">
              <Globe className="size-5 text-foreground-muted" />
              {d.hostname}
            </CardTitle>
            <div className="flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-foreground-muted">
              {summary.length ? (
                summary.map((s) => <span key={s}>{s}</span>)
              ) : (
                <span>还没配置账号, 先编辑域名</span>
              )}
              {d.note && <span>{d.note}</span>}
            </div>
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              disabled={full}
              onClick={onAddLine}
            >
              <Plus className="mr-1.5 size-3.5" />
              添加线路
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              onClick={onEditDomain}
              aria-label="编辑域名"
            >
              <Pencil className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              onClick={onDeleteDomain}
              aria-label="删除域名"
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {lines.length === 0 && (
          <div className="rounded-lg border border-dashed py-6 text-center text-sm text-foreground-muted">
            还没有线路
          </div>
        )}
        {lines.map((b) => (
          <BindingLane
            key={b.id}
            binding={b}
            nodes={nodes}
            busy={busy === b.id}
            onSwitch={onSwitch}
            onSync={onSync}
            onCheck={onCheck}
            onEdit={onEditLine}
            onDelete={onDeleteLine}
          />
        ))}
      </CardContent>
    </Card>
  );
}
