"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useToast } from "@/components/ui/use-toast";
import { fetchApi } from "@/lib/api-client";
import { type Credential, readError } from "../types";

// CredentialsCard 平台凭据: 密钥只写不读, 中控以 AES-GCM 加密保存
export function CredentialsCard({
  credentials,
  onChange,
}: {
  credentials: Credential[];
  onChange: () => void;
}) {
  const { toast } = useToast();
  const [kind, setKind] = useState<"cloudflare" | "dnspod">("cloudflare");
  const [name, setName] = useState("");
  const [apiToken, setApiToken] = useState("");
  const [secretId, setSecretId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetchApi("/admin/api/dns/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ kind, name, apiToken, secretId, secretKey }),
      });
      if (!res.ok) throw new Error(await readError(res));
      toast({ title: "凭据已保存" });
      setName("");
      setApiToken("");
      setSecretId("");
      setSecretKey("");
      onChange();
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

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">平台凭据</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-1 text-sm">
          {credentials.length === 0 && (
            <div className="text-muted-foreground">还没有凭据</div>
          )}
          {credentials.map((c) => (
            <div
              key={c.id}
              className="flex items-center justify-between border-b py-1.5 last:border-0"
            >
              <span>
                <span className="font-medium">{c.name}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  #{c.id} · {c.kind === "cloudflare" ? "Cloudflare" : "腾讯云"}
                </span>
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={() => remove(c)}
                aria-label="删除凭据"
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
        </div>

        <div className="grid gap-3 border-t pt-4 sm:grid-cols-2">
          <div className="space-y-1">
            <Label>类型</Label>
            <Select
              value={kind}
              onValueChange={(v) => setKind(v as "cloudflare" | "dnspod")}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="cloudflare">
                  Cloudflare (API Token)
                </SelectItem>
                <SelectItem value="dnspod">
                  腾讯云 DNSPod / EdgeOne (SecretId / SecretKey)
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label>名称</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如 CF 主账号"
            />
          </div>
          {kind === "cloudflare" ? (
            <div className="space-y-1 sm:col-span-2">
              <Label>API Token</Label>
              <Input
                type="password"
                value={apiToken}
                onChange={(e) => setApiToken(e.target.value)}
                autoComplete="off"
              />
            </div>
          ) : (
            <>
              <div className="space-y-1">
                <Label>SecretId</Label>
                <Input
                  value={secretId}
                  onChange={(e) => setSecretId(e.target.value)}
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1">
                <Label>SecretKey</Label>
                <Input
                  type="password"
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  autoComplete="off"
                />
              </div>
            </>
          )}
          <div className="sm:col-span-2">
            <Button onClick={save} disabled={saving || !name}>
              添加凭据
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
