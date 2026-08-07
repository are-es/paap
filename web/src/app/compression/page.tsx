"use client";

import { useState } from "react";
import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type SettingsData } from "@/lib/api";
import { Zap, Save, ScrollText } from "lucide-react";
import { cn } from "@/lib/utils";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { useLanguage } from "@/lib/language-context";

function fmtTokens(tokens: number): string {
  if (tokens < 1000) return tokens.toString();
  if (tokens < 1000000) return (tokens / 1000).toFixed(1) + "K";
  return (tokens / 1000000).toFixed(1) + "M";
}

const LEVELS = ["off", "lite", "medium", "high"] as const;
type Level = (typeof LEVELS)[number];

const LEVEL_META: Record<Level, { label: string; desc: string; color: string }> = {
  off:    { label: "Off",    desc: "No compression",                        color: "text-muted-foreground" },
  lite:   { label: "Lite",   desc: "10 tool outputs | ANSI strip + blank collapse",  color: "text-blue-500" },
  medium: { label: "Medium", desc: "20 tool + user | +line budget +prose +dedup",    color: "text-yellow-500" },
  high:   { label: "High",   desc: "30 all except assistant | +JSON/XML +trunc",     color: "text-green-500" },
};

export default function CompressionPage() {
  const queryClient = useQueryClient();
  const { t } = useLanguage();
  const [showDocs, setShowDocs] = useState(false);

  // System prompt injection
  const [injectionEnabled, setInjectionEnabled] = useState(false);
  const [injectionText, setInjectionText] = useState("");
  const [injectionPosition, setInjectionPosition] = useState<"prepend" | "append">("prepend");

  // Compression level
  const [level, setLevel] = useState<Level>("medium");

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });

  // Compression logs
  const logsQuery = useQuery({
    queryKey: ["compression-logs"],
    queryFn: () => api.getCompressionLogs({ limit: 300 }),
    refetchInterval: 5000,
  });

  // Sync form state from settings
  const prevSettingsRef = React.useRef<string>("");
  if (settingsQuery.data) {
    const d = settingsQuery.data as SettingsData;
    const sig = JSON.stringify({
      en: d.prompt_injection_enabled,
      txt: d.prompt_injection_text,
      pos: d.prompt_injection_position,
      lvl: d.compress_level,
    });
    if (sig !== prevSettingsRef.current) {
      prevSettingsRef.current = sig;
      if (d.prompt_injection_enabled !== undefined) {
        setInjectionEnabled(d.prompt_injection_enabled === true || String(d.prompt_injection_enabled) === "true");
      }
      if (d.prompt_injection_text !== undefined) {
        setInjectionText(String(d.prompt_injection_text || ""));
      }
      if (d.prompt_injection_position !== undefined) {
        setInjectionPosition((d.prompt_injection_position as "prepend" | "append") || "prepend");
      }
      if (d.compress_level) {
        const lvl = String(d.compress_level) as Level;
        if (LEVELS.includes(lvl)) {
          setLevel(lvl);
        }
      }
    }
  }

  const saveInjection = async (nextEnabled?: boolean, nextText?: string, nextPos?: "prepend" | "append") => {
    await api.updateSettings({
      prompt_injection_enabled: nextEnabled ?? injectionEnabled,
      prompt_injection_text: nextText ?? injectionText,
      prompt_injection_position: nextPos ?? injectionPosition,
    });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const toggleInjection = async () => {
    const next = !injectionEnabled;
    setInjectionEnabled(next);
    await saveInjection(next);
  };

  const selectLevel = async (newLevel: Level) => {
    setLevel(newLevel);
    await api.updateSettings({ compress_level: newLevel });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const logs = logsQuery.data ?? [];

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center justify-between mb-6">
        <h1 className="font-heading text-xl font-bold">Compression</h1>
        <DocsButton onClick={() => setShowDocs(true)} />
      </div>

      <div className="space-y-4">
        {/* SYSTEM PROMPT */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-sm font-medium">System Prompt</h2>
            <div className="flex items-center gap-3">
              <select
                value={injectionPosition}
                onChange={(e) => setInjectionPosition(e.target.value as "prepend" | "append")}
                className="px-2.5 py-1 text-xs rounded border border-input bg-background focus:outline-none focus:border-primary/50"
              >
                <option value="prepend">Prepend</option>
                <option value="append">Append</option>
              </select>
              <button
                onClick={toggleInjection}
                className={cn(
                  "w-9 h-5 rounded-full relative transition-colors",
                  injectionEnabled ? "bg-green-500" : "bg-foreground/15"
                )}
              >
                <span
                  className={cn(
                    "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
                    injectionEnabled ? "left-[18px]" : "left-[2px]"
                  )}
                />
              </button>
            </div>
          </div>

          <p className="text-[11px] text-muted-foreground mb-3">
            Teks yang di-inject ke setiap request sebelum dikirim ke provider
          </p>

          <textarea
            value={injectionText}
            onChange={(e) => setInjectionText(e.target.value)}
            rows={6}
            className="w-full px-3 py-2 text-xs rounded border border-input bg-background font-mono focus:outline-none focus:border-primary/50 resize-none mb-3"
            placeholder="Masukkan teks system prompt..."
          />

          <div className="flex justify-end">
            <button
              onClick={() => saveInjection()}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors font-medium"
            >
              <Save className="w-3 h-3" />
              Save
            </button>
          </div>
        </div>

        {/* COMPRESSION LEVEL — HORIZONTAL */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-4">
            <Zap className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Compression Level</span>
          </div>

          <div className="grid grid-cols-4 gap-2">
            {LEVELS.map((lvl) => {
              const meta = LEVEL_META[lvl];
              const active = level === lvl;
              return (
                <button
                  key={lvl}
                  onClick={() => selectLevel(lvl)}
                  className={cn(
                    "flex flex-col items-center gap-1.5 p-3 rounded-lg border transition-all text-center",
                    active
                      ? "border-primary/50 bg-primary/10 ring-1 ring-primary/20"
                      : "border-border hover:border-primary/20 hover:bg-primary/[0.02]"
                  )}
                >
                  <span className={cn("text-sm font-bold", active ? meta.color : "text-foreground")}>
                    {meta.label}
                  </span>
                  <span className="text-[10px] text-muted-foreground leading-tight">
                    {meta.desc}
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        {/* COMPRESSION LOGS */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <ScrollText className="w-4 h-4 text-muted-foreground" />
              <span className="text-sm font-medium">Compression Logs</span>
            </div>
            <button
              onClick={async () => {
                await fetch("/api/compression/logs", { method: "DELETE" });
                queryClient.invalidateQueries({ queryKey: ["compression-logs"] });
              }}
              className="px-2 py-1 text-[10px] rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/30 transition-colors"
            >
              Clear
            </button>
          </div>

          {logs.length === 0 ? (
            <p className="text-[11px] text-muted-foreground">No compression events yet.</p>
          ) : (
            <div className="max-h-64 overflow-y-auto overflow-x-auto rounded border border-border/50">
              <table className="w-full text-[11px] font-mono">
                <thead className="sticky top-0 bg-background z-10">
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="text-left py-1.5 px-2">Time</th>
                    <th className="text-left py-1.5 px-2">Type</th>
                    <th className="text-left py-1.5 px-2">Level</th>
                    <th className="text-right py-1.5 px-2">Before</th>
                    <th className="text-right py-1.5 px-2">After</th>
                    <th className="text-right py-1.5 px-2">Saved</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.map((log: any, i: number) => (
                    <tr key={i} className="border-b border-border/50 hover:bg-primary/[0.02]">
                      <td className="py-1 px-2 text-muted-foreground whitespace-nowrap">{new Date(log.timestamp).toLocaleString()}</td>
                      <td className="py-1 px-2">{log.content_type}</td>
                      <td className="py-1 px-2 capitalize">{log.level}</td>
                      <td className="py-1 px-2 text-right">{fmtTokens(log.original_tokens || Math.round(log.original_size / 4))}</td>
                      <td className="py-1 px-2 text-right">{fmtTokens(log.compressed_tokens || Math.round(log.compressed_size / 4))}</td>
                      <td className={`py-1 px-2 text-right ${log.original_size > 0 && (1 - log.compressed_size / log.original_size) * 100 > 10 ? "text-green-600" : "text-muted-foreground"}`}>
                        {log.original_size > 0
                          ? `${Math.round((1 - log.compressed_size / log.original_size) * 100)}%`
                          : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title="Compression"
        sections={[
          { title: "Overview", content: "Smart Compressor mengkompres message lama sebelum dikirim ke provider. Menghemat token dan biaya. Messages terbaru (6 terakhir) tetap utuh untuk jaga context." },
          { title: "Levels", content: "Off: tidak ada kompresi. Lite: 10 tool outputs terlama (ANSI strip + blank collapse). Medium: 20 tool + user messages (+line budget +prose filter +log dedup). High: 30 semua kecuali assistant (+JSON/XML compress +aggressive truncation)." },
          { title: "Yang Dilindungi", content: "Assistant messages TIDAK PERNAH di-compress — jawaban AI tetap murni. Recent messages (6 terakhir) di-skip untuk jaga context percakapan aktif." },
          { title: "Cara Kerja", content: "Setiap request, compressor ambil N message tertua yang eligible, compress paralel (goroutines), dan log hasilnya. Compression bersifat idempotent — aman dijalankan berulang." },
        ]}
      />
    </div>
  );
}
