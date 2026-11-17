"use client";

import { useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { type Credential, readError } from "../types";
import { CredentialDialog } from "./CredentialDialog";

// CredentialsCard 平台凭据列表: 新建 / 编辑走弹窗, 密钥只写不读 (中控以 AES-GCM 加密保存)
export function CredentialsCard({
  credentials,
  onChange,
}: {
  credentials: Credential[];
  onChange: () => void;
}) {
  const { toast } = useToast();
  const [dialog, setDialog] = useState<{
    open: boolean;
    credential: Credential | null;
  }>({
    open: false,
    credential: null,
  });

  const remove = async (c: Credential) => {
    if (!confirm(`删除凭据 ${c.name}?`)) return;
    const res = await fetchApi(`/admin/api/dns/credentials/${c.id}`, {
      method: "DELETE",
    });
    if (!res.ok)
      toast({
        title: "删除失败",
        description: await readError(res),
        variant: "destructive",
      });
    onChange();
  };

  const close = () => setDialog((d) => ({ ...d, open: false }));

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">平台凭据</CardTitle>
        <Button
          variant="outline"
          size="sm"
          className="h-8"
          onClick={() => setDialog({ open: true, credential: null })}
        >
          <Plus className="mr-1.5 size-3.5" />
          添加凭据
        </Button>
      </CardHeader>
      <CardContent className="text-sm">
        {credentials.length === 0 && (
          <div className="text-foreground-muted">还没有凭据</div>
        )}
        {credentials.map((c) => (
          <div
            key={c.id}
            className="flex items-center justify-between gap-3 border-b py-1.5 last:border-0"
          >
            <span className="min-w-0 truncate">
              <span className="font-medium">{c.name}</span>
              <span className="ml-2 text-xs text-foreground-muted">
                #{c.id} · {c.kind === "cloudflare" ? "Cloudflare" : "腾讯云"}
              </span>
            </span>
            <span className="flex shrink-0 items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={() => setDialog({ open: true, credential: c })}
                aria-label="编辑凭据"
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={() => remove(c)}
                aria-label="删除凭据"
              >
                <Trash2 className="size-3.5" />
              </Button>
            </span>
          </div>
        ))}
      </CardContent>
      <CredentialDialog
        open={dialog.open}
        credential={dialog.credential}
        onClose={close}
        onSaved={() => {
          close();
          onChange();
        }}
      />
    </Card>
  );
}
