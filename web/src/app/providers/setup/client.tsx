"use client";

import { useState, useMemo } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type ApiKeyItem, type PlaygroundResult } from "@/lib/api";
import {
  ProviderIcon,
  AuthTypeBadge,
  ProviderTypeBadge,
  StatusPill,
} from "@/components/providers/provider-helpers";
import {
  ChevronRight,
  Key,
  Plus,
  Trash2,
  Copy,
  Check,
  Play,
  Loader2,
  ChevronDown,
  Wand2,
  X,
  Eye,
  EyeOff,
  ArrowLeft,
  Globe,
} from "lucide-react";
import { cn } from "@/lib/utils";

export function ProviderSetupClient() {
  const searchParams = useSearchParams();
  const providerId = searchParams.get("id") ?? "";
  const queryClient = useQueryClient();

  const providerQuery = useQuery({
    queryKey: ["provider", providerId],
    queryFn: () => api.getProvider(providerId),
    enabled: providerId !== "",
  });

  const provider = providerQuery.data;

  if (providerQuery.isLoading) {
    return (
      <div className="p-6 md:p-8 min-h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-neon-cyan" />
      </div>
    );
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/providers" className="inline-flex items-center gap-1 hover:text-neon-cyan transition-colors">
          <ArrowLeft className="w-3.5 h-3.5" />
          Providers
        </Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">{provider?.name ?? "..."}</span>
      </nav>

      <div className="flex items-center gap-4 mb-8">
        {provider && <ProviderIcon provider={provider} size="lg" />}
        <div className="flex-1">
          <h1 className="font-heading text-xl font-bold">{provider?.name ?? "..."}</h1>
          <div className="flex items-center gap-2 mt-1">
            <span className="text-[11px] font-mono text-muted-foreground">{provider?.base_url}</span>
          </div>
          <div className="flex items-center gap-2 mt-2">
            <AuthTypeBadge authType={provider?.auth_type ?? "apikey"} />
            <ProviderTypeBadge providerType={provider?.provider_type ?? "custom"} />
            <StatusPill status={provider?.status ?? "offline"} />
          </div>
        </div>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 cursor-pointer">
            <span className="text-[11px] text-muted-foreground font-medium">Online</span>
            <ToggleSwitch
              enabled={provider?.is_active ?? false}
              onChange={() => {
                // Toggle provider active status
                fetch(`/api/providers/${providerId}/toggle-active`, { method: "POST" })
                  .then(() => {
                    queryClient.invalidateQueries({ queryKey: ["provider", providerId] });
                    queryClient.invalidateQueries({ queryKey: ["providers"] });
                  });
              }}
            />
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <span className="text-[11px] text-muted-foreground font-medium">RR</span>
            <ToggleSwitch
              enabled={provider?.round_robin_enabled ?? provider?.round_robin ?? false}
              onChange={() => {
                const current = provider?.round_robin_enabled ?? provider?.round_robin ?? false;
                api.toggleRoundRobin(providerId, !current)
                  .then(() => {
                    queryClient.invalidateQueries({ queryKey: ["provider", providerId] });
                    queryClient.invalidateQueries({ queryKey: ["providers"] });
                  });
              }}
            />
          </label>
        </div>
      </div>

      {provider?.auth_type === "connection" ? (
        <ConnectionsSection providerId={providerId} />
      ) : (
        <KeysSection providerId={providerId} />
      )}
      <ModelsSection providerId={providerId} />
      <PlaygroundSection providerId={providerId} />
    </div>
  );
}

function ToggleSwitch({ enabled, onChange }: { enabled: boolean; onChange: () => void }) {
  return (
    <button
      onClick={onChange}
      className={cn(
        "w-9 h-5 rounded-full relative transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        enabled
          ? "bg-green-500"
          : "bg-foreground/15"
      )}
    >
      <span
        className={cn(
          "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
            enabled
              ? "left-[18px]"
              : "left-[2px]"
        )}
      />
    </button>
  );
}

function NeonCollapse({
  title,
  icon,
  count,
  children,
  defaultOpen = false,
  accentColor = "cyan",
  action,
}: {
  title: string;
  icon: React.ReactNode;
  count?: number;
  children: React.ReactNode;
  defaultOpen?: boolean;
  accentColor?: "cyan" | "magenta" | "green" | "amber" | "purple";
  action?: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className="mb-4 border border-border rounded-lg bg-card overflow-hidden transition-all">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-muted/30 transition-colors"
      >
        <span className="text-muted-foreground">{icon}</span>
        <span className="font-medium text-sm flex-1 text-left">{title}</span>
        {count !== undefined && (
          <span className="text-[11px] font-mono text-muted-foreground">{count}</span>
        )}
        {action}
        <span className="text-muted-foreground transition-transform duration-200" style={{ transform: open ? "rotate(180deg)" : "rotate(0deg)" }}>
          <ChevronDown className="w-4 h-4" />
        </span>
      </button>
      <div
        className="overflow-hidden transition-all duration-300 ease-in-out"
        style={{ maxHeight: open ? "2000px" : "0px", opacity: open ? 1 : 0 }}
      >
        <div className="px-4 pb-4 border-t border-border">{children}</div>
      </div>
    </div>
  );
}

function AddKeyModal({ onClose, providerId }: { onClose: () => void; providerId: string }) {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<"single" | "bulk">("single");
  const [singleKey, setSingleKey] = useState("");
  const [bulkText, setBulkText] = useState("");

  // derive next key name from existing keys
  const keysQuery = useQuery({
    queryKey: ["keys", providerId],
    queryFn: () => api.getKeys(providerId),
  });
  const existingKeys = keysQuery.data ?? [];

  const nextKeyNum = useMemo(() => {
    let max = 0;
    for (const k of existingKeys) {
      const m = String(k.name || "").match(/(\d+)\s*$/);
      if (m) {
        const n = parseInt(m[1], 10);
        if (n > max) max = n;
      }
    }
    return max + 1;
  }, [existingKeys]);

  const bulkLines = useMemo(() => bulkText.split("\n").map(s => s.trim()).filter(Boolean), [bulkText]);

  function previewKeyName(index: number): string {
    return `key-${nextKeyNum + index}`;
  }

  const addMutation = useMutation({
    mutationFn: (payload: { key: string; name?: string }) => api.addKey(providerId, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["keys", providerId] });
      setSingleKey("");
    },
  });

  const bulkMutation = useMutation({
    mutationFn: (keys: { key: string; name: string }[]) => api.bulkAddKeysWithNames(providerId, keys),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["keys", providerId] });
      setBulkText("");
      onClose();
    },
  });

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={onClose}
    >
      <div
        className="bg-popover border border-border rounded-xl w-full max-w-md mx-4 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h3 className="font-medium text-sm text-neon-cyan">Add API Key</h3>
          <button onClick={onClose} className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="flex border-b border-border">
          {(["single", "bulk"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={cn(
                "flex-1 px-4 py-2 text-sm font-medium transition-colors",
                tab === t
                  ? "border-b-2 border-neon-cyan text-neon-cyan"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {t === "single" ? "Single Key" : "Bulk Add"}
            </button>
          ))}
        </div>

        <div className="p-4">
          {tab === "single" ? (
            <div>
              <p className="text-[11px] text-muted-foreground mb-2">Next name: <span className="font-mono text-foreground">{previewKeyName(0)}</span></p>
              <input
                placeholder="Paste API key..."
                value={singleKey}
                onChange={(e) => setSingleKey(e.target.value)}
                className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm font-mono placeholder:text-muted-foreground/70 focus:outline-none focus:border-neon-cyan/50"
                autoFocus
              />
              <button
                onClick={() => { if (singleKey.trim()) addMutation.mutate({ key: singleKey.trim(), name: previewKeyName(0) }); }}
                disabled={!singleKey.trim() || addMutation.isPending}
                className="mt-3 w-full px-3 py-2 text-sm rounded-lg bg-primary text-primary-foreground font-medium disabled:opacity-40 disabled:bg-muted disabled:text-muted-foreground hover:shadow-[0_0_12px_rgba(0,240,255,0.3)] transition-all"
              >
                {addMutation.isPending ? "Adding..." : `Add ${previewKeyName(0)}`}
              </button>
            </div>
          ) : (
            <div>
              {bulkLines.length > 0 && (
                <p className="text-[11px] text-muted-foreground mb-2">Will create: <span className="font-mono text-foreground">{previewKeyName(0)} .. {previewKeyName(bulkLines.length-1)}</span></p>
              )}
              <textarea
                placeholder="One API key per line..."
                value={bulkText}
                onChange={(e) => setBulkText(e.target.value)}
                rows={6}
                className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm font-mono focus:outline-none focus:border-neon-cyan/50"
                autoFocus
              />
              <button
                onClick={() => {
                  const ks = bulkLines.map((k,i)=>({ key: k, name: previewKeyName(i) }));
                  if (ks.length > 0) bulkMutation.mutate(ks);
                }}
                disabled={!bulkText.trim() || bulkMutation.isPending}
                className="mt-3 w-full px-3 py-2 text-sm rounded-lg bg-primary text-primary-foreground font-medium disabled:opacity-50 hover:shadow-[0_0_12px_rgba(0,240,255,0.3)] transition-shadow"
              >
                {bulkMutation.isPending ? "Adding..." : `Add ${bulkLines.length} Keys`}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function KeysSection({ providerId }: { providerId: string }) {
  const queryClient = useQueryClient();
  const [copiedId, setCopiedId] = useState<number | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [visibleKeyId, setVisibleKeyId] = useState<number | null>(null);

  const keysQuery = useQuery({
    queryKey: ["keys", providerId],
    queryFn: () => api.getKeys(providerId),
  });

  const deleteMutation = useMutation({
    mutationFn: (keyId: number) => api.deleteKey(providerId, keyId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["keys", providerId] }),
  });

  const toggleMutation = useMutation({
    mutationFn: (keyId: number) => api.toggleKey(providerId, keyId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["keys", providerId] }),
  });

  const deleteDisabledMutation = useMutation({
    mutationFn: () => api.deleteDisabledKeys(providerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["keys", providerId] }),
  });

  const enableAllMutation = useMutation({
    mutationFn: () => api.enableAllKeys(providerId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["keys", providerId] }),
  });

  const keys = keysQuery.data ?? [];

  const copyKey = (key: ApiKeyItem) => {
    const val = key.key || key.key_masked || "";
    navigator.clipboard.writeText(val);
    setCopiedId(key.id);
    setTimeout(() => setCopiedId(null), 2000);
  };

  const doMaskKey = (key: string) => {
    if (!key) return "••••••••";
    if (key.length <= 12) return key;
    return key.slice(0, 8) + "••••••••" + key.slice(-4);
  };

  return (
    <NeonCollapse
      title="API Keys"
      icon={<Key className="w-4 h-4" />}
      count={keys.length}
          defaultOpen
          action={
            <div className="flex gap-2">
              <button
                onClick={(e) => { e.stopPropagation(); setShowModal(true); }}
                className="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors font-medium"
              >
                <Plus className="w-3 h-3" />
                Add Key
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); enableAllMutation.mutate(); }}
                className="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] rounded border border-green-500/20 text-green-600 bg-green-500/5 hover:bg-green-500/10 transition-colors font-medium"
              >
                Enable All
              </button>
              <button
                onClick={(e) => { e.stopPropagation(); deleteDisabledMutation.mutate(); }}
                className="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] rounded border border-red-500/20 text-red-500 bg-red-500/5 hover:bg-red-500/10 transition-colors font-medium"
              >
                <Trash2 className="w-3 h-3" />
                Delete Disabled
              </button>
            </div>
          }
        >
      <div className="space-y-2 mt-3">
        {keys.map((key) => {
          const isVisible = visibleKeyId === key.id;
          const displayKey = isVisible ? (key.key || "") : doMaskKey(key.key || key.key_masked || "");

          return (
            <div
              key={key.id}
              className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-border/50 bg-secondary/60 hover:bg-secondary/80 border-secondary-foreground/10 transition-colors"
            >
              <button
                onClick={() => toggleMutation.mutate(key.id)}
                className={cn(
                  "w-8 h-[18px] rounded-full relative transition-colors shrink-0",
                  key.is_active
                    ? "bg-green-500"
                    : "bg-foreground/15"
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-3.5 h-3.5 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
                    key.is_active
                      ? "left-[16px]"
                      : "left-0.5"
                  )}
                />
              </button>
              <Key className="w-4 h-4 text-neon-cyan/50 shrink-0" />
              <div className="flex-1 min-w-0">
                <div className="text-sm font-medium">{key.name || "Unnamed"}</div>
                <div className="font-mono text-xs text-muted-foreground truncate">{displayKey}</div>
              </div>
              {key.fail_count != null && key.fail_count > 0 && (
                <span className="px-1.5 py-0.5 rounded text-[10px] bg-neon-magenta/10 text-neon-magenta border border-neon-magenta/20 font-mono">
                  {key.fail_count} fails
                </span>
              )}
              <button
                onClick={() => setVisibleKeyId(isVisible ? null : key.id)}
                className="p-1 hover:bg-muted rounded text-muted-foreground hover:text-foreground"
                title={isVisible ? "Hide key" : "Show key"}
              >
                {isVisible ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={() => copyKey(key)}
                className="p-1 hover:bg-muted rounded text-muted-foreground hover:text-foreground"
                title="Copy key"
              >
                {copiedId === key.id ? <Check className="w-3.5 h-3.5 text-neon-green" /> : <Copy className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={() => deleteMutation.mutate(key.id)}
                className="p-1 hover:bg-neon-magenta/10 rounded text-neon-magenta"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          );
        })}
        {keys.length === 0 && (
          <p className="text-sm text-muted-foreground py-3 text-center">No keys yet</p>
        )}
      </div>

      {showModal && <AddKeyModal onClose={() => setShowModal(false)} providerId={providerId} />}
    </NeonCollapse>
  );
}

function ConnectionsSection({ providerId }: { providerId: string }) {
  const queryClient = useQueryClient();
  const [oauthFlow, setOauthFlow] = useState<{
    verification_uri: string;
    verification_uri_complete: string;
    user_code: string;
    expires_in: number;
  } | null>(null);
  const [polling, setPolling] = useState(false);
  const [oauthError, setOauthError] = useState("");

  const connectionsQuery = useQuery({
    queryKey: ["connections", providerId],
    queryFn: () => api.getConnections(providerId),
  });

  const deleteMutation = useMutation({
    mutationFn: (connId: number) => api.deleteConnection(providerId, connId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["connections", providerId] }),
  });

  const toggleMutation = useMutation({
    mutationFn: (connId: number) =>
      fetch(`/api/providers/${providerId}/connections/${connId}`, { method: "POST" }).then(r => r.json()),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["connections", providerId] }),
  });

  const isGoogleOAuth = providerId.includes("anigravity");

  const startOAuth = async () => {
    setOauthError("");
    try {
      const res = await fetch(`/api/oauth/${providerId}/device-code`, { method: "POST" });
      const data = await res.json();
      if (res.ok) {
        setOauthFlow(data);
        setPolling(true);
        pollForToken(data.interval || 5);
      } else {
        setOauthError(data.error || "Failed to start OAuth flow");
      }
    } catch (e: unknown) {
      setOauthError(e instanceof Error ? e.message : "Connection failed");
    }
  };

  const startGoogleOAuth = () => {
    window.location.href = `/api/oauth/anigravity/start`;
  };

  const pollForToken = async (intervalSec: number) => {
    const poll = async () => {
      try {
        const res = await fetch(`/api/oauth/${providerId}/poll`, { method: "POST" });
        const data = await res.json();
        if (data.status === "connected") {
          setOauthFlow(null);
          setPolling(false);
          queryClient.invalidateQueries({ queryKey: ["connections", providerId] });
          return;
        }
        if (data.status === "pending" || data.status === "slow_down") {
          setTimeout(poll, (data.status === "slow_down" ? intervalSec + 5 : intervalSec) * 1000);
          return;
        }
        setOauthError(data.error || "Authorization failed");
        setOauthFlow(null);
        setPolling(false);
      } catch {
        setTimeout(poll, intervalSec * 1000);
      }
    };
    poll();
  };

  const connections = connectionsQuery.data ?? [];

  return (
    <NeonCollapse title="Connections" icon={<Globe className="w-4 h-4" />} count={connections.length} defaultOpen accentColor="purple">
      <div className="space-y-2 mt-3">
        {connections.map((conn) => (
          <div key={conn.id} className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-border bg-secondary/60">
            <button
              onClick={() => toggleMutation.mutate(conn.id)}
              className={cn(
                "w-8 h-[18px] rounded-full relative transition-colors shrink-0",
                conn.is_active
                  ? "bg-green-500"
                  : "bg-foreground/15"
              )}
            >
              <span
                className={cn(
                  "absolute top-0.5 w-3.5 h-3.5 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
                  conn.is_active
                    ? "left-[16px]"
                    : "left-0.5"
                )}
              />
            </button>
            <div className="flex-1">
              <div className="text-sm font-medium">{conn.name}</div>
              {conn.email && <div className="text-xs text-muted-foreground">{conn.email}</div>}
            </div>
            <button
              onClick={() => deleteMutation.mutate(conn.id)}
              className="p-1 hover:bg-destructive/10 rounded text-muted-foreground hover:text-destructive"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
      </div>

      {oauthFlow ? (
        <div className="mt-3 p-4 rounded-lg border border-neon-cyan/20 bg-neon-cyan/5">
          <div className="text-sm font-medium mb-2 text-neon-cyan">Authorize in browser</div>
          <div className="space-y-2">
            <div>
              <span className="text-xs text-muted-foreground">URL: </span>
              <a href={oauthFlow.verification_uri_complete} target="_blank" rel="noopener" className="text-sm text-neon-cyan hover:underline font-mono">
                {oauthFlow.verification_uri}
              </a>
            </div>
            <div>
              <span className="text-xs text-muted-foreground">Code: </span>
              <span className="text-lg font-mono font-bold tracking-wider text-neon-cyan">{oauthFlow.user_code}</span>
            </div>
            {polling && (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Loader2 className="w-3 h-3 animate-spin text-neon-cyan" />
                Waiting for authorization...
              </div>
            )}
          </div>
          <button onClick={() => { setOauthFlow(null); setPolling(false); }} className="mt-3 text-xs text-muted-foreground hover:text-foreground">
            Cancel
          </button>
        </div>
      ) : (
        <button
          onClick={isGoogleOAuth ? startGoogleOAuth : startOAuth}
          className="mt-3 inline-flex items-center gap-1.5 px-3 py-2 text-sm rounded-lg border border-border hover:border-neon-purple/40 hover:bg-neon-purple/5 text-muted-foreground hover:text-neon-purple transition-all"
        >
          <Plus className="w-3.5 h-3.5" /> {isGoogleOAuth ? "Connect Google" : "Connect"}
        </button>
      )}

      {oauthError && (
        <div className="mt-2 text-xs text-neon-magenta">{oauthError}</div>
      )}
    </NeonCollapse>
  );
}

function ModelsSection({ providerId }: { providerId: string }) {
  const queryClient = useQueryClient();
  const [detecting, setDetecting] = useState(false);
  const [newModel, setNewModel] = useState("");

  const modelsQuery = useQuery({
    queryKey: ["models", providerId],
    queryFn: () => api.getModels(providerId),
  });

  const detectMutation = useMutation({
    mutationFn: () => api.detectModels(providerId),
    onMutate: () => setDetecting(true),
    onSettled: () => setDetecting(false),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["models", providerId] }),
  });

  const addModelMutation = useMutation({
    mutationFn: (modelId: string) =>
      fetch(`/api/providers/${providerId}/models`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model_id: modelId }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["models", providerId] });
      setNewModel("");
    },
  });

  const updateMutation = useMutation({
    mutationFn: (modelIds: string[]) => api.updateModels(providerId, modelIds),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["models", providerId] }),
  });

  const removeMutation = useMutation({
    mutationFn: (modelId: string) =>
      fetch(`/api/providers/${providerId}/models/${encodeURIComponent(modelId)}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["models", providerId] }),
  });

  const models = modelsQuery.data ?? [];

  const toggleModel = (modelId: string) => {
    const current = models.filter((m) => m.selected).map((m) => m.name);
    const next = current.includes(modelId) ? current.filter((id) => id !== modelId) : [...current, modelId];
    updateMutation.mutate(next);
  };

  return (
    <NeonCollapse title="Models" icon={<Wand2 className="w-4 h-4" />} count={models.length} accentColor="green">
      <div className="flex items-center justify-between mb-3 mt-3">
        <span className="text-sm text-muted-foreground">
          <span className="text-neon-green font-mono">{models.filter((m) => m.selected).length}</span> selected
        </span>
        <button
          onClick={() => detectMutation.mutate()}
          disabled={detecting}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg border border-border hover:border-neon-green/40 hover:bg-neon-green/5 disabled:opacity-50 transition-all"
        >
          {detecting ? <Loader2 className="w-3.5 h-3.5 animate-spin text-neon-green" /> : <Wand2 className="w-3.5 h-3.5 text-neon-green" />}
          Detect Models
        </button>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {models.map((model) => (
          <div
            key={model.id}
            className={cn(
              "inline-flex items-center gap-1 px-2.5 py-1 rounded text-[11px] font-mono transition-all border cursor-pointer",
              model.selected
                ? "bg-primary/10 text-primary border-primary/30 font-medium"
                : "border-border text-muted-foreground hover:text-foreground hover:border-primary/20"
            )}
          >
            <button onClick={() => toggleModel(model.name)} className="hover:underline">
              {model.name}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); removeMutation.mutate(model.name); }}
              className="ml-0.5 hover:text-neon-magenta transition-colors"
              title="Remove model"
            >
              <X className="w-3 h-3" />
            </button>
          </div>
        ))}
        {models.length === 0 && (
          <p className="text-sm text-muted-foreground py-2">No models detected. Click Detect Models to scan.</p>
        )}
      </div>
      <div className="flex gap-2 mt-3">
        <input
          placeholder="Model name (e.g. gpt-4o, deepseek-v3)"
          value={newModel}
          onChange={(e) => setNewModel(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && newModel.trim()) addModelMutation.mutate(newModel.trim()); }}
          className="flex-1 px-3 py-1.5 rounded-lg border border-input bg-background text-sm font-mono focus:outline-none focus:border-neon-cyan/50"
        />
        <button
          onClick={() => { if (newModel.trim()) addModelMutation.mutate(newModel.trim()); }}
          disabled={!newModel.trim() || addModelMutation.isPending}
          className="inline-flex items-center gap-1 px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground font-medium disabled:opacity-40 disabled:bg-muted disabled:text-muted-foreground hover:shadow-[0_0_12px_rgba(0,240,255,0.2)] transition-all"
        >
          <Plus className="w-3.5 h-3.5" />
          Add
        </button>
      </div>
    </NeonCollapse>
  );
}
function PlaygroundSection({ providerId }: { providerId: string }) {
const [keyId, setKeyId] = useState<string>();
const [model, setModel] = useState("");
const [prompt, setPrompt] = useState("hi");
const [results, setResults] = useState<PlaygroundResult[] | null>(null);
const [error, setError] = useState<string | null>(null);
const queryClient = useQueryClient();

const keysQuery = useQuery({ queryKey: ["keys", providerId], queryFn: () => api.getKeys(providerId) });
const modelsQuery = useQuery({ queryKey: ["models", providerId], queryFn: () => api.getModels(providerId) });
const connectionsQuery = useQuery({ queryKey: ["connections", providerId], queryFn: () => api.getConnections(providerId) });

const testMutation = useMutation({
mutationFn: () => api.runPlayground(providerId, { key_id: keyId, model_id: model, prompt }),
onSuccess: (data: PlaygroundResult & { results?: PlaygroundResult[] }) => {
setError(null);
if (data.results && Array.isArray(data.results)) {
setResults(data.results);
} else {
setResults([{ status: data.status, latency_ms: data.latency_ms, res: data.res, key: data.key || "" }]);
}
},
onError: (err: Error) => { setError(err.message); setResults(null); },
});

const activeKeys = keysQuery.data?.filter((k) => k.is_active) ?? [];
const activeConnections = connectionsQuery.data?.filter((c) => c.is_active) ?? [];
const selectedModels = modelsQuery.data?.filter((m) => m.selected) ?? [];

return (
<NeonCollapse title="Playground" icon={<Play className="w-4 h-4" />} accentColor="amber">
<div className="space-y-3 mt-3">
<div className="grid grid-cols-2 gap-3">
<select
value={keyId ?? ""}
onChange={(e) => setKeyId(e.target.value || undefined)}
className="px-3 py-2 rounded-lg border border-input bg-background text-sm focus:outline-none focus:border-neon-amber/50"
>
<option value="">All Keys</option>
{activeKeys.map((k) => <option key={k.id} value={k.id}>{k.name || k.key.slice(0, 12) + "..."}</option>)}
{activeConnections.map((c) => <option key={c.id} value={c.id}>{c.name || c.email || c.id}</option>)}
</select>
          <select
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="px-3 py-2 rounded-lg border border-input bg-background text-sm focus:outline-none focus:border-neon-amber/50"
          >
            <option value="">Select model...</option>
            {selectedModels.map((m) => <option key={m.id} value={m.name}>{m.name}</option>)}
          </select>
        </div>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={2}
          className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm focus:outline-none focus:border-neon-amber/50"
        />
        <button
          onClick={() => testMutation.mutate()}
          disabled={testMutation.isPending || !model}
          className="inline-flex items-center gap-1.5 px-4 py-2 text-sm rounded-lg bg-primary text-primary-foreground font-medium disabled:opacity-40 disabled:bg-muted disabled:text-muted-foreground hover:shadow-[0_0_12px_rgba(0,240,255,0.2)] transition-all"
        >
          {testMutation.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />} Run Test
        </button>

        {error && (
          <div className="p-3 rounded-lg text-sm bg-neon-magenta/10 border border-neon-magenta/20 text-neon-magenta">
            <div className="whitespace-pre-wrap">{error}</div>
          </div>
        )}

        {results && results.map((r, i) => (
          <div
            key={i}
            className={cn(
              "p-3 rounded-lg text-sm border",
              r.status >= 200 && r.status < 300
                ? "bg-neon-green/5 border-neon-green/20"
                : "bg-neon-magenta/5 border-neon-magenta/20"
            )}
          >
            {r.key && <div className="text-xs font-medium text-muted-foreground mb-1 font-mono">{r.key}</div>}
            <div className="whitespace-pre-wrap">{r.res}</div>
            <div className="text-xs text-muted-foreground mt-1 font-mono">
              <span className={r.status >= 200 && r.status < 300 ? "text-neon-green" : "text-neon-magenta"}>{r.status}</span>
              {" — "}
              <span className="text-neon-amber">{r.latency_ms}ms</span>
            </div>
          </div>
        ))}
      </div>
    </NeonCollapse>
  );
}
