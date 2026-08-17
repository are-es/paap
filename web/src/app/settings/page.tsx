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
  Languages,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { ConfirmModal } from "@/components/ui/confirm-modal";
import { useLanguage, LANGUAGES } from "@/lib/language-context";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";

const INTERVAL_OPTIONS = [
  { value: "3", label: "3 min" },
  { value: "5", label: "5 min" },
  { value: "10", label: "10 min" },
  { value: "15", label: "15 min" },
  { value: "30", label: "30 min" },
  { value: "60", label: "60 min" },
];

export default function SettingsPage() {
  const { lang, setLang, t } = useLanguage();
  const queryClient = useQueryClient();
  const [showDocs, setShowDocs] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [shuttingDown, setShuttingDown] = useState(false);
  const [confirmAction, setConfirmAction] = useState<null | "clear" | "restart" | "shutdown">(null);

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

  const handleConfirm = async () => {
    if (confirmAction === "clear") {
      setClearing(true);
      setConfirmAction(null);
      await api.clearAll();
      queryClient.invalidateQueries({ queryKey: ["logs"] });
      queryClient.invalidateQueries({ queryKey: ["cost"] });
      setClearing(false);
    } else if (confirmAction === "restart") {
      setRestarting(true);
      setConfirmAction(null);
      await api.restart();
    } else if (confirmAction === "shutdown") {
      setShuttingDown(true);
      setConfirmAction(null);
      await api.shutdown();
    }
  };

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center justify-between mb-6">
        <h1 className="font-heading text-xl font-bold">{t("settings.title")}</h1>
        <DocsButton onClick={() => setShowDocs(true)} />
      </div>

      <div className="space-y-4">
        {/* CARD 0: LANGUAGE */}
        <div className="bg-card border border-border rounded-xl p-5">
          <div className="flex items-center gap-2 mb-1">
            <Languages className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">{t("settings.language")}</span>
          </div>
          <p className="text-[11px] text-muted-foreground mb-3">
            {t("settings.languageDesc")}
          </p>
          <select
            value={lang}
            onChange={(e) => setLang(e.target.value as any)}
            className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none"
          >
            {LANGUAGES.map((l) => (
              <option key={l.code} value={l.code}>
                {l.flag} {l.label}
              </option>
            ))}
          </select>
        </div>

        {/* CARD 1: PROXY */}
        <div className="bg-card border border-border rounded-xl p-5">
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
            className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none"
          >
            {INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </div>

        {/* CARD 2: DATA */}
        <div className="bg-card border border-border rounded-xl p-5">
          <div className="flex items-center gap-2 mb-3">
            <Trash2 className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Data Management</span>
          </div>
          <p className="text-[11px] text-muted-foreground mb-3 max-w-prose">
            Hapus semua log, cost data, dan usage stats. API keys dan providers TIDAK terpengaruh.
          </p>
          <button
            onClick={() => setConfirmAction("clear")}
            disabled={clearing}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg border border-destructive/30 text-destructive hover:bg-destructive/10 disabled:opacity-50"
          >
            {clearing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
            {clearing ? "Clearing..." : "Clear All Data"}
          </button>
        </div>

        {/* CARD 3: SERVER */}
        <div className="bg-card border border-border rounded-xl p-5">
          <div className="flex items-center gap-2 mb-3">
            <Power className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Server Control</span>
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => setConfirmAction("restart")}
              disabled={restarting}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg border border-amber-500/30 text-amber-500 hover:bg-amber-500/10 disabled:opacity-50"
            >
              {restarting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCcw className="w-3.5 h-3.5" />}
              {restarting ? "Restarting..." : "Restart"}
            </button>
            <button
              onClick={() => setConfirmAction("shutdown")}
              disabled={shuttingDown}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg border border-destructive/30 text-destructive hover:bg-destructive/10 disabled:opacity-50"
            >
              {shuttingDown ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Power className="w-3.5 h-3.5" />}
              {shuttingDown ? "Shutting down..." : "Shutdown"}
            </button>
          </div>
        </div>
      </div>

      <ConfirmModal
        open={confirmAction !== null}
        title={
          confirmAction === "clear" ? "Clear All Data" :
          confirmAction === "restart" ? "Restart Server" :
          "Shutdown Server"
        }
        message={
          confirmAction === "clear" ? "Clear all logs, cost data, and usage stats? API keys and providers will NOT be affected." :
          confirmAction === "restart" ? "Restart PAAP server? Connections will be briefly interrupted." :
          "Shutdown PAAP server? You will lose connection."
        }
        confirmLabel={
          confirmAction === "clear" ? "Clear All" :
          confirmAction === "restart" ? "Restart" :
          "Shutdown"
        }
        variant={confirmAction === "restart" ? "default" : "danger"}
        onConfirm={handleConfirm}
        onCancel={() => setConfirmAction(null)}
      />

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title={t("settings_docs_title")}
        sections={[
          { title: t("settings_docs_overview_title"), content: t("settings_docs_overview_content") },
          { title: t("settings_docs_language_title"), content: t("settings_docs_language_content") },
          { title: t("settings_docs_data_title"), content: t("settings_docs_data_content") },
          { title: t("settings_docs_server_title"), content: t("settings_docs_server_content") },
          { title: t("settings_docs_troubleshoot_title"), content: t("settings_docs_troubleshoot_content") },
        ]}
      />
    </div>
  );
}
