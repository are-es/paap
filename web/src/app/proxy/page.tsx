"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type ProxyItem } from "@/lib/api";
import {
  Plus,
  Trash2,
  Play,
  Loader2,
  X,
} from "lucide-react";
import { cn } from "@/lib/utils";

export default function ProxyPage() {
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);
  const [testingAll, setTestingAll] = useState(false);
  const [testResults, setTestResults] = useState<Array<{ ip: string; latency: number; country: string; timedOut: boolean }> | null>(null);

  const proxiesQuery = useQuery({
    queryKey: ["proxies"],
    queryFn: () => api.getProxies(),
  });

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });

  const proxyEnabled = String(settingsQuery.data?.proxy_enabled) === "true";

  const toggleProxy = async () => {
    await api.updateSettings({ proxy_enabled: !proxyEnabled });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteProxy(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["proxies"] }),
  });

  const handleTestAll = async () => {
    setTestingAll(true);
    setTestResults(null);
    try {
      const res = await fetch("/api/proxies/test-all", { method: "POST" });
      if (!res.ok) throw new Error("Test failed");
      const data = await res.json();
      const results = (data.results || []).map((r: { id: string; test_ip?: string; latency_ms?: number; country?: string; test_status?: string }) => {
        const proxy = proxies.find(p => String(p.id) === r.id);
        const timedOut = r.test_status === "failed" || (r.latency_ms ?? 0) > 2000;
        return {
          ip: r.test_ip || (proxy ? `${proxy.address}:${proxy.port}` : "?"),
          latency: timedOut ? 0 : (r.latency_ms ?? 0),
          country: timedOut ? "-" : (r.country ?? "-"),
          timedOut,
        };
      });
      setTestResults(results);
    } catch (e) {
      console.error("Test failed:", e);
      setTestResults([]);
    }
    setTestingAll(false);
    queryClient.invalidateQueries({ queryKey: ["proxies"] });
  };

  const handleDeleteOffline = async () => {
    const offlineProxies = proxies.filter((p) => p.status === "inactive");
    if (offlineProxies.length === 0) return;
    const results = await Promise.allSettled(
      offlineProxies.map((p) => api.deleteProxy(p.id))
    );
    queryClient.invalidateQueries({ queryKey: ["proxies"] });
    const failed = results.filter((r) => r.status === "rejected").length;
    if (failed > 0) {
      alert(`Failed to delete ${failed} of ${offlineProxies.length} offline proxies.`);
    }
  };

  const proxies = proxiesQuery.data ?? [];
  const offlineCount = proxies.filter((p) => p.status === "inactive").length;

  return (
    <div className="p-6 md:p-8 min-h-full flex flex-col gap-5">
      {/* HEADER */}
      <div className="flex items-center justify-between">
        <h1 className="font-heading text-xl font-bold">Proxy</h1>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-2 cursor-pointer">
            <span className="text-[11px] text-muted-foreground font-medium">Active</span>
            <button
              onClick={toggleProxy}
              className={cn(
                "w-9 h-5 rounded-full relative transition-colors",
                proxyEnabled ? "bg-green-500" : "bg-gray-300 dark:bg-gray-600"
              )}
            >
              <span className={cn(
                "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white",
                proxyEnabled ? "left-[18px]" : "left-[2px]"
              )} />
            </button>
          </label>
          <button
            onClick={handleTestAll}
            disabled={testingAll || proxies.length === 0}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-colors disabled:opacity-50"
          >
            {testingAll ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}
            Test Manual
          </button>
          <button
            onClick={handleDeleteOffline}
            disabled={offlineCount === 0}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded border border-destructive/20 text-destructive bg-destructive/5 hover:bg-destructive/10 transition-colors disabled:opacity-50"
          >
            <Trash2 className="w-3 h-3" />
            Delete All Offline
          </button>
          <button
            onClick={() => setAddOpen(true)}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors font-medium"
          >
            <Plus className="w-3 h-3" />
            Add Proxy
          </button>
        </div>
      </div>

      {/* PROXY TABLE — fixed height, scrollable */}
      <div className="bg-card border border-border rounded-lg overflow-hidden flex-1 min-h-0">
        <div className="overflow-y-auto" style={{ height: "calc(100vh - 220px)" }}>
          <table className="w-full text-xs">
            <thead className="sticky top-0 z-10 bg-card/95 backdrop-blur-sm">
              <tr className="border-b border-border">
                <th className="px-3 py-2 text-left font-medium text-muted-foreground">IP</th>
                <th className="px-3 py-2 text-left font-medium text-muted-foreground">Type</th>
                <th className="px-3 py-2 text-left font-medium text-muted-foreground">Country</th>
                <th className="px-3 py-2 text-right font-medium text-muted-foreground">Latency</th>
                <th className="px-3 py-2 text-center font-medium text-muted-foreground">Status</th>
                <th className="px-3 py-2 text-right font-medium text-muted-foreground"></th>
              </tr>
            </thead>
            <tbody>
              {proxies.length === 0 && !proxiesQuery.isLoading && (
                <tr>
                  <td colSpan={6} className="text-center py-12 text-muted-foreground">
                    No proxies configured
                  </td>
                </tr>
              )}
              {proxies.map((proxy) => (
                <tr key={proxy.id} className="border-b border-border/30 hover:bg-muted/30 transition-colors">
                  <td className="px-3 py-1.5 font-mono">{proxy.address}:{proxy.port}</td>
                  <td className="px-3 py-1.5 text-muted-foreground uppercase">{proxy.type}</td>
                  <td className="px-3 py-1.5 text-muted-foreground">{proxy.country ?? "-"}</td>
                  <td className="px-3 py-1.5 text-right font-mono">
                    {proxy.latency_ms != null ? (
                      <span className={cn(
                        proxy.latency_ms < 200 ? "text-green-600" :
                        proxy.latency_ms < 500 ? "text-amber-600" :
                        "text-red-500"
                      )}>
                        {proxy.latency_ms}ms
                      </span>
                    ) : "-"}
                  </td>
                  <td className="px-3 py-1.5 text-center">
                    <span className={cn(
                      "inline-flex items-center gap-1",
                      proxy.status === "active" ? "text-green-600" : "text-red-400"
                    )}>
                      <span className={cn(
                        "w-1.5 h-1.5 rounded-full",
                        proxy.status === "active" ? "bg-green-500" : "bg-red-400"
                      )} />
                      {proxy.status === "active" ? "Online" : "Offline"}
                    </span>
                  </td>
                  <td className="px-3 py-1.5 text-right">
                    <button
                      onClick={() => deleteMutation.mutate(proxy.id)}
                      className="p-1 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* ADD PROXY MODAL */}
      <AddProxyModal open={addOpen} onClose={() => setAddOpen(false)} />

      {/* TEST RESULTS POPUP */}
      {testResults && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setTestResults(null)}>
          <div className="bg-card border border-border rounded-lg shadow-xl w-full max-w-sm" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <h3 className="text-sm font-semibold">Test Results</h3>
              <button onClick={() => setTestResults(null)} className="p-1 hover:bg-muted rounded">
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="p-3 space-y-1.5 max-h-80 overflow-y-auto">
              {testResults.length === 0 && (
                <p className="text-sm text-muted-foreground text-center py-4">No results</p>
              )}
              {testResults.map((r, i) => (
                <div key={i} className="flex items-center gap-3 px-3 py-2 rounded border border-border/50">
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-mono font-medium">{r.ip}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {r.timedOut ? "- · -" : `${r.latency}ms · ${r.country}`}
                    </div>
                  </div>
                  <span className={cn(
                    "w-1.5 h-1.5 rounded-full",
                    r.timedOut ? "bg-red-400" : "bg-green-500"
                  )} />
                </div>
              ))}
            </div>
            <div className="px-4 py-3 border-t border-border">
              <button
                onClick={() => setTestResults(null)}
                className="w-full px-3 py-1.5 text-xs rounded border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function AddProxyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<"single" | "bulk">("single");
  const [form, setForm] = useState({ address: "", port: "", type: "socks5" });
  const [bulkText, setBulkText] = useState("");

  const addMutation = useMutation({
    mutationFn: () => api.addProxy({ address: form.address, port: parseInt(form.port), type: form.type as "socks5" | "http" | "https" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setForm({ address: "", port: "", type: "socks5" });
      onClose();
    },
  });

  const bulkMutation = useMutation({
    mutationFn: async () => {
      const lines = bulkText.trim().split("\n").filter(Boolean);
      for (const line of lines) {
        const parts = line.trim().split(":");
        if (parts.length >= 2) {
          await api.addProxy({ address: parts[0], port: parseInt(parts[1]), type: form.type as "socks5" | "http" | "https" });
        }
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["proxies"] });
      setBulkText("");
      onClose();
    },
  });

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div className="bg-card border border-border rounded-lg shadow-xl w-full max-w-md" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h3 className="text-sm font-semibold">Add Proxy</h3>
          <button onClick={onClose} className="p-1 hover:bg-muted rounded"><X className="w-4 h-4" /></button>
        </div>
        <div className="flex border-b border-border px-4">
          <button onClick={() => setTab("single")} className={cn("px-3 py-2 text-xs font-medium border-b-2 transition-colors", tab === "single" ? "border-primary text-primary" : "border-transparent text-muted-foreground")}>Single</button>
          <button onClick={() => setTab("bulk")} className={cn("px-3 py-2 text-xs font-medium border-b-2 transition-colors", tab === "bulk" ? "border-primary text-primary" : "border-transparent text-muted-foreground")}>Bulk</button>
        </div>
        <div className="p-4 space-y-3">
          {tab === "single" ? (
            <>
              <div className="flex gap-2">
                <div className="flex-1">
                  <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 block">IP</label>
                  <input placeholder="103.21.58.1" value={form.address} onChange={(e) => setForm({ ...form, address: e.target.value })} className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background focus:outline-none focus:border-primary/50" />
                </div>
                <div className="w-20">
                  <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 block">Port</label>
                  <input placeholder="8080" value={form.port} onChange={(e) => setForm({ ...form, port: e.target.value })} className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background focus:outline-none focus:border-primary/50" />
                </div>
              </div>
              <div>
                <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 block">Type</label>
                <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background focus:outline-none focus:border-primary/50">
                  <option value="socks5">SOCKS5</option>
                  <option value="http">HTTP</option>
                  <option value="https">HTTPS</option>
                </select>
              </div>
            </>
          ) : (
            <>
              <div>
                <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 block">Proxies (one per line, ip:port)</label>
                <textarea value={bulkText} onChange={(e) => setBulkText(e.target.value)} rows={5} className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background font-mono focus:outline-none focus:border-primary/50 resize-none" placeholder={"103.21.58.1:8080\n45.76.102.3:3128"} />
              </div>
              <div>
                <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1 block">Type</label>
                <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background focus:outline-none focus:border-primary/50">
                  <option value="socks5">SOCKS5</option>
                  <option value="http">HTTP</option>
                  <option value="https">HTTPS</option>
                </select>
              </div>
            </>
          )}
        </div>
        <div className="flex justify-end gap-2 px-4 py-3 border-t border-border">
          <button onClick={onClose} className="px-3 py-1.5 text-xs rounded border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-colors">Cancel</button>
          <button onClick={() => tab === "single" ? addMutation.mutate() : bulkMutation.mutate()} disabled={tab === "single" ? (!form.address || !form.port) : !bulkText.trim()} className="px-3 py-1.5 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors font-medium disabled:opacity-30">
            {tab === "single" ? "Add" : "Add All"}
          </button>
        </div>
      </div>
    </div>
  );
}
