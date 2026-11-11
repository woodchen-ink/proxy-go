"use client";

import { ChevronRight, Pencil, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatTime } from "@/lib/format";
import {
  bindingTarget,
  bindingTitle,
  type Binding,
  type BindingCheck,
  type NodeOption,
} from "../types";

interface Props {
  binding: Binding;
  nodes: NodeOption[];
  busy: boolean;
  onSwitch: (b: Binding, nodeId: string) => void;
  onCheck: (b: Binding) => void;
  onEdit: (b: Binding) => void;
  onDelete: (b: Binding) => void;
}

function SyncBadge({ check }: { check: BindingCheck | null }) {
  if (!check)
    return (
      <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
        未检查
      </span>
    );
  const err = check.cf_error || check.dnspod_error || check.eo_error;
  if (err) {
    return (
      <span
        className="rounded-full bg-destructive-soft px-2 py-0.5 text-xs text-destructive"
        title={err}
      >
        读取失败
      </span>
    );
  }
  return check.consistent ? (
    <span className="rounded-full bg-success-soft px-2 py-0.5 text-xs text-success">
      线上一致
    </span>
  ) : (
    <span className="rounded-full bg-warning-soft px-2 py-0.5 text-xs text-warning">
      与配置不一致
    </span>
  );
}

// BindingLane 一条线路的分流视图: 节点池按优先级横排, 当前承接流量的节点高亮, 点击节点即切换
export function BindingLane({
  binding: b,
  nodes,
  busy,
  onSwitch,
  onCheck,
  onEdit,
  onDelete,
}: Props) {
  const isEO = b.kind === "edgeone";
  const check = b.last_check;
  const actual = isEO ? check?.eo_origin : check?.cf_origin;

  return (
    <div className={`rounded-lg border p-4 ${b.enabled ? "" : "opacity-60"}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">{bindingTitle(b)}</span>
          <SyncBadge check={check} />
          <span className="text-xs text-muted-foreground">
            故障切换{b.auto_failover ? "开" : "关"} · 自动切回
            {b.auto_failback ? "开" : "关"}
          </span>
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            className="h-8"
            disabled={busy}
            onClick={() => onCheck(b)}
          >
            <RefreshCw
              className={`mr-1.5 size-3.5 ${busy ? "animate-spin" : ""}`}
            />
            检查
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            onClick={() => onEdit(b)}
            aria-label="编辑"
          >
            <Pencil className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            onClick={() => onDelete(b)}
            aria-label="删除"
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-stretch gap-2">
        {b.pool.map((id, i) => {
          const n = nodes.find((x) => x.node_id === id);
          const active = id === b.active_node;
          const online = !!n?.online;
          return (
            <div key={id} className="flex items-center gap-2">
              {i > 0 && (
                <ChevronRight
                  aria-hidden
                  className="size-4 text-muted-foreground"
                />
              )}
              <button
                type="button"
                disabled={busy || active || !b.enabled}
                onClick={() => onSwitch(b, id)}
                title={active ? "当前承接流量" : "切换到这个节点"}
                className={`min-w-[190px] rounded-lg border px-3 py-2.5 text-left transition-colors ${
                  active
                    ? "border-foreground bg-foreground/[0.03] ring-1 ring-foreground"
                    : "hover:border-foreground/40 hover:bg-accent disabled:hover:bg-transparent"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs text-muted-foreground">
                    {i === 0 ? "首选" : `备选 ${i}`}
                  </span>
                  {active && (
                    <span className="rounded bg-foreground px-1.5 py-0.5 text-[10px] text-background">
                      当前
                    </span>
                  )}
                </div>
                <div className="mt-1 flex items-center gap-1.5 font-mono text-sm">
                  <span
                    aria-hidden
                    className={`size-2 shrink-0 rounded-full ${online ? "bg-success" : "bg-destructive"}`}
                  />
                  {id}
                </div>
                <div className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
                  {bindingTarget(b.kind, n) || "未登记"}
                </div>
              </button>
            </div>
          );
        })}
      </div>

      <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {isEO ? (
          <span>
            EdgeOne 站点{" "}
            <span className="font-mono">
              {b.saas_zone_name || b.saas_zone_id}
            </span>{" "}
            · 实际源站 <span className="font-mono">{actual || "-"}</span>
          </span>
        ) : b.kind === "dnspod_ip" ? (
          <span>
            DNSPod{" "}
            <span className="font-mono">
              {b.record_name === "@" ? "" : `${b.record_name}.`}
              {b.dnspod_domain}
            </span>{" "}
            · 实际解析{" "}
            <span className="font-mono">{check?.dnspod_value || "-"}</span>
          </span>
        ) : (
          <>
            <span>
              CF 回源{" "}
              <span className="font-mono">{check?.cf_origin || "-"}</span>
            </span>
            <span>
              DNSPod{" "}
              <span className="font-mono">{check?.dnspod_value || "-"}</span>
            </span>
          </>
        )}
        {check && <span>检查于 {formatTime(check.at)}</span>}
        {(check?.cf_error || check?.dnspod_error || check?.eo_error) && (
          <span className="text-destructive">
            {[check.cf_error, check.dnspod_error, check.eo_error]
              .filter(Boolean)
              .join("; ")}
          </span>
        )}
      </div>
    </div>
  );
}
