"use client";

import { ArrowDown, ArrowUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { type NodeOption } from "../types";

// PoolEditor 有序节点池: 第一个是首选; 只能加入 target(n) 非空的节点
export function PoolEditor({
  pool,
  nodes,
  target,
  onChange,
}: {
  pool: string[];
  nodes: NodeOption[];
  target: (n?: NodeOption) => string;
  onChange: (pool: string[]) => void;
}) {
  const move = (idx: number, delta: number) => {
    const j = idx + delta;
    if (j < 0 || j >= pool.length) return;
    const next = [...pool];
    [next[idx], next[j]] = [next[j], next[idx]];
    onChange(next);
  };
  const candidates = nodes.filter(
    (n) => target(n) && !pool.includes(n.node_id),
  );

  return (
    <div className="space-y-1 rounded-md border p-2">
      {pool.length === 0 && (
        <div className="px-1 py-1 text-xs text-foreground-muted">
          从下方加入节点, 第一个是首选
        </div>
      )}
      {pool.map((id, idx) => (
        <div key={id} className="flex items-center gap-2 text-sm">
          <span className="w-5 text-foreground-muted">{idx + 1}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs">
            {id} → {target(nodes.find((n) => n.node_id === id)) || "未登记"}
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
            onClick={() => onChange(pool.filter((p) => p !== id))}
          >
            移出
          </Button>
        </div>
      ))}
      {candidates.length > 0 && (
        <div className="flex flex-wrap gap-2 pt-1">
          {candidates.map((n) => (
            <Button
              key={n.node_id}
              variant="outline"
              size="sm"
              className="h-7 font-mono text-xs"
              onClick={() => onChange([...pool, n.node_id])}
            >
              + {n.node_id}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
}
