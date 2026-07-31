"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  Globe,
  Trash2,
  RotateCcw,
  Power,
  Loader2,
} from "lucide-react";
import { cn } from "@/lib/utils";

const INTERVAL_OPTIONS = [
  { value: "3", label: "3 min" },
  { value: "5", label: "5 min" },
  { value: "10", label: "10 min" },
  { value: "15", label: "15 min" },
  { value: "30", label: "30 min" },
  { value: "60", label: "60 min" },
];

export default function SettingsPage() {
  const queryClient = useQueryClient();
  const [clearing, setClearing] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [shuttingDown, setShuttingDown] = useState(false);

  const [proxyInterval, setProxyInterval] = useState<string | null>(null);

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });

  const currentInterval = proxyInterval ?? String(settingsQuery.data?.proxy_test_interval ?? "3");

  const updateInterval = async (val: string) => {
    setProxyInterval(val);
    await api.updateSettings({ proxy_test_interval: val });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const handleClearAll = async () => {
    if (!confirm("Clear all logs, cost data, and usage stats? API keys and providers will NOT be affected.")) return;
    setClearing(true);
    await api.clearLogs();
    queryClient.invalidateQueries({ queryKey: ["logs"] });
    queryClient.invalidateQueries({ queryKey: ["cost"] });
    setClearing(false);
  };

  const handleRestart = async () => {
    if (!confirm("Restart PAAP server? Connections will be briefly interrupted.")) return;
    setRestarting(true);
    await api.restart();
  };

  const handleShutdown = async () => {
    if (!confirm("Shutdown PAAP server? You will lose connection.")) return;
    setShuttingDown(true);
    await api.shutdown();
  };

  return (
    <div className="p-6 md:p-8 min-h-full">
      <h1 className="font-heading text-xl font-bold mb-6">Settings</h1>

      <div className="space-y-4">
        {/* CARD 1: PROXY */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-3">
            <Globe className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Proxy Auto-Test</span>
          </div>
          <p className="text-[11px] text-muted-foreground mb-3">
            Interval pengujian otomatis semua proxy yang aktif
          </p>
          <select
            value={currentInterval}
            onChange={(e) => updateInterval(e.target.value)}
            className="w-full px-2.5 py-1.5 text-xs rounded border border-input bg-background font-mono focus:outline-none focus:border-primary/50"
          >
            {INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </div>

        {/* CARD 2: CLEAR ALL */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-3">
            <Trash2 className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Clear All Data</span>
          </div>
          <p className="text-[11px] text-muted-foreground mb-3">
            Hapus semua logs, cost data, dan usage stats. API keys, providers, dan proxy TIDAK akan dihapus.
          </p>
          <button
            onClick={handleClearAll}
            disabled={clearing}
            className="w-full inline-flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs rounded border border-destructive/20 text-destructive bg-destructive/5 hover:bg-destructive/10 transition-colors font-medium disabled:opacity-50"
          >
            {clearing ? <Loader2 className="w-3 h-3 animate-spin" /> : <Trash2 className="w-3 h-3" />}
            Reset All Data
          </button>
        </div>

        {/* CARD 3: SERVER */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <Power className="w-4 h-4 text-muted-foreground" />
              <span className="text-sm font-medium">Server</span>
            </div>
            <span className="inline-flex items-center gap-1 text-[10px] font-medium text-green-600">
              <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />
              Active
            </span>
          </div>
          <p className="text-[11px] text-muted-foreground mb-3">
            Restart atau matikan server PAAP
          </p>
          <div className="flex gap-2">
            <button
              onClick={handleRestart}
              disabled={restarting}
              className="flex-1 inline-flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs rounded border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-colors disabled:opacity-50"
            >
              {restarting ? <Loader2 className="w-3 h-3 animate-spin" /> : <RotateCcw className="w-3 h-3" />}
              Restart
            </button>
            <button
              onClick={handleShutdown}
              disabled={shuttingDown}
              className="flex-1 inline-flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs rounded border border-destructive/20 text-destructive bg-destructive/5 hover:bg-destructive/10 transition-colors disabled:opacity-50"
            >
              {shuttingDown ? <Loader2 className="w-3 h-3 animate-spin" /> : <Power className="w-3 h-3" />}
              Shutdown
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
