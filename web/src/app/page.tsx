"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { ProviderTopology } from "@/components/dashboard/topology";
import { Modal } from "@/components/ui/modal";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { Plus, Trash2, Copy, Check, Eye, EyeOff } from "lucide-react";
import { useLanguage } from "@/lib/language-context";
import { cn } from "@/lib/utils";

export default function DashboardPage() {
  const queryClient = useQueryClient();
  const { t } = useLanguage();
  const [showAddKey, setShowAddKey] = useState(false);
  const [keyName, setKeyName] = useState("");
  const [copied, setCopied] = useState<number | null>(null);
  const [showKeyId, setShowKeyId] = useState<number | null>(null);
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null);
  const [showDocs, setShowDocs] = useState(false);

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.settings(),
  });

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => api.providers(),
    refetchInterval: 30_000,
  });

  const gatewayKeysQuery = useQuery({
    queryKey: ["gateway-keys"],
    queryFn: () => api.getGatewayKeys(),
  });

  const costQuery = useQuery({
    queryKey: ["cost"],
    queryFn: () => api.getCost(),
    refetchInterval: 30_000,
  });

  const addKeyMutation = useMutation({
    mutationFn: () => api.addGatewayKey({ name: keyName }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["gateway-keys"] });
      setShowAddKey(false);
      setKeyName("");
    },
  });

  const deleteKeyMutation = useMutation({
    mutationFn: (id: number) => api.deleteGatewayKey(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["gateway-keys"] }),
  });

  const copyToClipboard = async (text: string, id: number) => {
    await navigator.clipboard.writeText(text);
    setCopied(id);
    setTimeout(() => setCopied(null), 2000);
  };

  const settings = settingsQuery.data;
  const providers = providersQuery.data ?? [];
  const gatewayKeys = gatewayKeysQuery.data ?? [];
  const cost = costQuery.data;

  const formatCost = (val?: number) => {
    if (val === undefined || val === null) return "$0.00";
    if (val < 0.01) return "<$0.01";
    return `$${val.toFixed(2)}`;
  };

  const formatTokens = (val?: number) => {
    if (!val) return "0";
    if (val >= 1_000_000) return `${(val / 1_000_000).toFixed(1)}M`;
    if (val >= 1_000) return `${(val / 1_000).toFixed(1)}K`;
    return val.toString();
  };

  const stats = [
    { label: "Requests (24h)", value: cost?.summary?.today?.req_count?.toString() ?? "0" },
    { label: "Tokens In (24h)", value: formatTokens(cost?.summary?.today?.tokens_in) },
    { label: "Cost (24h)", value: formatCost(cost?.summary?.today?.cost_usd) },
    { label: "Active Providers", value: providers.filter(p => p.status === "online").length.toString() },
  ];

  return (
    <div className="min-h-screen bg-background">
      <main className="px-4 md:px-8 py-6">
        <div className="flex flex-wrap items-center justify-between gap-2 mb-6">
          <h1 className="font-heading text-xl font-bold text-foreground">Dashboard</h1>
          <div className="flex items-center gap-2">
            <DocsButton onClick={() => setShowDocs(true)} />
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3 mb-6">
          {stats.map((stat) => (
            <div
              key={stat.label}
              className="bg-card border border-border rounded-lg p-3.5 hover:border-primary/40 transition-colors"
            >
              <p className="text-xs text-muted-foreground mb-1">{stat.label}</p>
              <p className="font-mono text-lg font-bold text-foreground">{stat.value}</p>
            </div>
          ))}
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3 mb-6">
          <section className="bg-card border border-border rounded-lg p-3">
            <h2 className="text-xs text-muted-foreground mb-1.5">Base URL</h2>
            <div className="relative">
              <code className="block font-mono text-xs bg-muted/50 border border-border rounded px-2 py-1.5 text-foreground break-all">
                {settings?.base_url ?? "Not configured"}
              </code>
              {settings?.base_url && (
                <button
                  onClick={() => copyToClipboard(settings.base_url!, -1)}
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                  aria-label="Copy base URL"
                >
                  {copied === -1 ? (
                    <Check className="w-3 h-3 text-green-500" />
                  ) : (
                    <Copy className="w-3 h-3" />
                  )}
                </button>
              )}
            </div>
          </section>

          <section className="bg-card border border-border rounded-lg p-3">
            <h2 className="text-xs text-muted-foreground mb-1.5">Gateway API Key</h2>
            {gatewayKeys.length > 0 ? (
              <div className="space-y-1.5">
                <div className="flex items-center gap-1.5">
                  <div className="relative flex-1 min-w-0">
                    <select
                      className="w-full font-mono text-xs bg-muted/50 border border-border rounded px-2 py-1.5 text-foreground appearance-none cursor-pointer pr-6 truncate focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none"
                      onChange={(e) => {
                        const key = gatewayKeys.find(k => k.id === Number(e.target.value));
                        if (key) setSelectedKeyId(key.id);
                      }}
                      value={selectedKeyId ?? gatewayKeys[0]?.id}
                    >
                      {gatewayKeys.map(k => {
                        const masked = k.key.slice(0, 3) + "••••••••" + k.key.slice(-4);
                        return <option key={k.id} value={k.id}>{showKeyId === k.id ? k.key : `${k.name}, ${masked}`}</option>;
                      })}
                    </select>
                    <svg className="absolute right-1.5 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="m6 9 6 6 6-6"/></svg>
                  </div>
                  <button
                    onClick={() => setShowKeyId(showKeyId ? null : (selectedKeyId ?? gatewayKeys[0]?.id ?? null))}
                    className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                    title={showKeyId ? "Hide key" : "Show key"}
                  >
                    {showKeyId ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                  </button>
                  <button
                    onClick={() => {
                      const key = gatewayKeys.find(k => k.id === (selectedKeyId ?? gatewayKeys[0]?.id));
                      if (key) copyToClipboard(key.key, -2);
                    }}
                    className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                    title="Copy key"
                  >
                    {copied === -2 ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <div className="flex items-center gap-1.5">
                  <button
                    onClick={() => setShowAddKey(true)}
                    className="inline-flex items-center gap-0.5 px-2 py-0.5 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors"
                  >
                    <Plus className="w-2.5 h-2.5" />
                    Generate
                  </button>
                  <button
                    onClick={() => {
                      const keyId = selectedKeyId ?? gatewayKeys[0]?.id;
                      if (keyId) deleteKeyMutation.mutate(keyId);
                    }}
                    disabled={deleteKeyMutation.isPending}
                    className="inline-flex items-center gap-0.5 px-2 py-0.5 text-xs rounded border border-destructive/20 text-destructive bg-destructive/5 hover:bg-destructive/10 transition-colors disabled:opacity-50"
                  >
                    <Trash2 className="w-2.5 h-2.5" />
                    Revoke
                  </button>
                </div>
              </div>
            ) : (
              <div className="text-center py-2 text-muted-foreground">
                <p className="text-xs mb-1.5">No gateway key configured</p>
                <button
                  onClick={() => setShowAddKey(true)}
                  className="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-all"
                >
                  <Plus className="w-2.5 h-2.5" />
                  Generate Key
                </button>
              </div>
            )}
          </section>
        </div>

        <div className="mb-6 min-w-0">
          <ProviderTopology providers={providers} />
        </div>

        <Modal open={showAddKey} onClose={() => setShowAddKey(false)} title="Add Gateway Key">
          <div className="space-y-4">
            <div>
              <label className="text-xs text-muted-foreground font-semibold mb-2 block">
                Key Name
              </label>
              <input
                placeholder="e.g. hermes, jcode, dev"
                value={keyName}
                onChange={(e) => setKeyName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && keyName) addKeyMutation.mutate();
                }}
                className="w-full px-3.5 py-2.5 rounded-lg border border-input bg-background text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none transition-all"
                autoFocus
              />
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => setShowAddKey(false)}
                className="px-4 py-2 text-sm rounded-lg border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-all"
              >
                Cancel
              </button>
              <button
                onClick={() => addKeyMutation.mutate()}
                disabled={!keyName || addKeyMutation.isPending}
                className={cn(
                  "px-4 py-2 text-sm rounded-lg border transition-all duration-200",
                  "border-primary/25 text-primary bg-primary/10 hover:bg-primary/15 hover:border-primary/35",
                  "disabled:opacity-30 disabled:cursor-not-allowed"
                )}
              >
                {addKeyMutation.isPending ? "Creating..." : "Create Key"}
              </button>
            </div>
          </div>
        </Modal>

        <DocsModal
          open={showDocs}
          onClose={() => setShowDocs(false)}
          title={t("dashboard_docs_title")}
          sections={[
            { title: t("dashboard_docs_overview_title"), content: t("dashboard_docs_overview_content") },
            { title: t("dashboard_docs_keys_title"), content: t("dashboard_docs_keys_content") },
            { title: t("dashboard_docs_topology_title"), content: t("dashboard_docs_topology_content") },
            { title: t("dashboard_docs_troubleshoot_title"), content: t("dashboard_docs_troubleshoot_content") },
          ]}
        />
      </main>
    </div>
  );
}
