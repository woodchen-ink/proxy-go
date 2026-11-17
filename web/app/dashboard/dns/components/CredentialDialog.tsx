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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { type Credential, readError } from "../types";

type Kind = Credential["kind"];

interface Props {
  open: boolean;
  credential: Credential | null; // null = 新建
  onClose: () => void;
  onSaved: () => void;
}

// CredentialDialog 新建 / 编辑平台凭据; 密钥只写不读, 编辑时留空表示不改
export function CredentialDialog({
  open,
  credential,
  onClose,
  onSaved,
}: Props) {
  const { toast } = useToast();
  const [kind, setKind] = useState<Kind>("cloudflare");
  const [name, setName] = useState("");
  const [apiToken, setApiToken] = useState("");
  const [secretId, setSecretId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [saving, setSaving] = useState(false);
  const editing = !!credential;

  useEffect(() => {
    if (!open) return;
    setKind(credential?.kind ?? "cloudflare");
    setName(credential?.name ?? "");
    setApiToken("");
    setSecretId("");
    setSecretKey("");
  }, [open, credential]);

  // 腾讯云密钥是一对, 只填一半等于没填, 提前拦住
  const halfPair =
    kind === "dnspod" && !!secretId.trim() !== !!secretKey.trim();
  const missingSecret =
    !editing &&
    (kind === "cloudflare"
      ? !apiToken.trim()
      : !secretId.trim() || !secretKey.trim());

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetchApi("/admin/api/dns/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          id: credential?.id,
          kind,
          name,
          apiToken,
          secretId,
          secretKey,
        }),
      });
      if (!res.ok) throw new Error(await readError(res));
      toast({ title: editing ? "凭据已更新" : "凭据已添加" });
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

  const keep = editing ? "留空不改" : undefined;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {editing ? `编辑 ${credential.name}` : "添加凭据"}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {!editing && (
            <Tabs value={kind} onValueChange={(v) => setKind(v as Kind)}>
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="cloudflare">Cloudflare</TabsTrigger>
                <TabsTrigger value="dnspod">
                  腾讯云 DNSPod / EdgeOne
                </TabsTrigger>
              </TabsList>
            </Tabs>
          )}
          <div className="space-y-1">
            <Label>名称</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={
                kind === "cloudflare" ? "如 CF 主账号" : "如 腾讯云主账号"
              }
            />
          </div>
          {kind === "cloudflare" ? (
            <div className="space-y-1">
              <Label>API Token</Label>
              <Input
                type="password"
                value={apiToken}
                onChange={(e) => setApiToken(e.target.value)}
                placeholder={keep}
                autoComplete="off"
              />
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>SecretId</Label>
                <Input
                  value={secretId}
                  onChange={(e) => setSecretId(e.target.value)}
                  placeholder={keep}
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1">
                <Label>SecretKey</Label>
                <Input
                  type="password"
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  placeholder={keep}
                  autoComplete="off"
                />
              </div>
            </div>
          )}
          {editing && (
            <p className="text-xs text-foreground-muted">
              密钥加密保存且不回显; 只改名称时密钥留空即可, 换密钥需要整组重填。
            </p>
          )}
          {halfPair && (
            <p className="text-xs text-destructive">
              SecretId 与 SecretKey 需要一起填写
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={save}
            disabled={saving || !name.trim() || halfPair || missingSecret}
          >
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
