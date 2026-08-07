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
  lite:   { label: "Lite",   desc: "25 tool outputs | ANSI strip + blank collapse",  color: "text-blue-500" },
  medium: { label: "Medium", desc: "50 tool + user | +line budget +prose +dedup",    color: "text-yellow-500" },
  high:   { label: "High",   desc: "100 all except assistant | +JSON/XML +BM25",     color: "text-green-500" },
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

        {/* BEFORE / AFTER SUMMARY */}
        {logs.length > 0 && (() => {
          const totalOrig = logs.reduce((sum: number, l: any) => sum + (l.original_tokens || Math.round((l.original_size || 0) / 4)), 0);
          const totalSaved = logs.reduce((sum: number, l: any) => sum + (l.saved_tokens || Math.round(((l.original_size || 0) - (l.compressed_size || 0)) / 4)), 0);
          const totalAfter = totalOrig - totalSaved;
          const pct = totalOrig > 0 ? Math.round((totalSaved / totalOrig) * 100) : 0;
          return (
            <div className="grid grid-cols-3 gap-3">
              <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4 text-center">
                <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Before</p>
                <p className="font-mono text-xl font-bold text-foreground">{fmtTokens(totalOrig)}</p>
                <p className="text-[10px] text-muted-foreground">tokens</p>
              </div>
              <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4 text-center">
                <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">After</p>
                <p className="font-mono text-xl font-bold text-foreground">{fmtTokens(totalAfter)}</p>
                <p className="text-[10px] text-muted-foreground">tokens</p>
              </div>
              <div className="bg-green-500/[0.04] border border-green-500/15 rounded-lg p-4 text-center">
                <p className="text-[10px] font-semibold text-green-600 uppercase tracking-wider mb-1">Saved</p>
                <p className="font-mono text-xl font-bold text-green-600">{fmtTokens(totalSaved)}</p>
                <p className="text-[10px] text-green-600">{pct}%</p>
              </div>
            </div>
          );
        })()}
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
