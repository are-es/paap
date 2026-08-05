"use client";

import { useState } from "react";
import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type SettingsData } from "@/lib/api";
import { Zap, Save, Gauge } from "lucide-react";
import { cn } from "@/lib/utils";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { useLanguage } from "@/lib/language-context";

interface CompressionMode {
  id: string;
  label: string;
  description: string;
  available: boolean;
}

const COMPRESSION_MODES: CompressionMode[] = [
  {
    id: "caveman",
    label: "Caveman",
    description: "Drop filler/articles/hedging. Keep technical substance exact. Terse prose.",
    available: true,
  },
  {
    id: "rtk",
    label: "RTK",
    description: "Rust Token Killer. Compress tool outputs (bash, grep, git). 60-90% reduction on tool output.",
    available: true,
  },
];

const LEVELS = ["lite", "full", "ultra"] as const;
type Level = (typeof LEVELS)[number];

// Parse "caveman:ultra,ponytail:full" → { caveman: "ultra", ponytail: "full" }
function parseModes(raw: string): Record<string, Level> {
  if (!raw) return {};
  const result: Record<string, Level> = {};
  for (const part of raw.split(",")) {
    const [mode, level] = part.trim().split(":");
    if (mode && level && LEVELS.includes(level as Level)) {
      result[mode] = level as Level;
    } else if (mode) {
      result[mode] = "full"; // default
    }
  }
  return result;
}

// Serialize { caveman: "ultra", ponytail: "full" } → "caveman:ultra,ponytail:full"
function serializeModes(modes: Record<string, Level>): string {
  return Object.entries(modes)
    .map(([mode, level]) => `${mode}:${level}`)
    .join(",");
}

export default function SkillsPage() {
  const queryClient = useQueryClient();
  const { t } = useLanguage();
  const [injectionEnabled, setInjectionEnabled] = useState(false);
  const [injectionText, setInjectionText] = useState("");
  const [injectionPosition, setInjectionPosition] = useState<"prepend" | "append">("prepend");
  const [injectionLoaded, setInjectionLoaded] = useState(false);
  const [showDocs, setShowDocs] = useState(false);

  // Compression mode state — per-mode levels
  const [activeModes, setActiveModes] = useState<Record<string, Level>>({});

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });

  const headroomEnabled = String(settingsQuery.data?.headroom_enabled ?? "false") === "true";

  // Polled only while enabled: the answer can change without PAAP doing anything
  // (user starts the proxy in a terminal), and it is dead weight while off.
  const headroomQuery = useQuery({
    queryKey: ["headroom-status"],
    queryFn: () => api.headroomStatus(),
    enabled: headroomEnabled,
    refetchInterval: headroomEnabled ? 10_000 : false,
  });

  // Sync local form state from server whenever settings are (re)fetched.
  // Previously this was guarded by !injectionLoaded, so toggles that wrote to
  // the server and then invalidated the query would never re-sync, making it
  // look like the toggle "bounced back" on refresh.
  const prevSettingsRef = React.useRef<string>("");
  if (settingsQuery.data) {
    const d = settingsQuery.data as SettingsData;
    const sig = JSON.stringify({
      en: d.prompt_injection_enabled,
      txt: d.prompt_injection_text,
      pos: d.prompt_injection_position,
      mode: d.compression_mode,
    });
    if (sig !== prevSettingsRef.current) {
      prevSettingsRef.current = sig;
      if (d.prompt_injection_enabled !== undefined) {
        const v = d.prompt_injection_enabled;
        setInjectionEnabled(v === true || String(v) === "true");
      }
      if (d.prompt_injection_text !== undefined) {
        setInjectionText(String(d.prompt_injection_text || ""));
      }
      if (d.prompt_injection_position !== undefined) {
        setInjectionPosition((d.prompt_injection_position as "prepend" | "append") || "prepend");
      }
      if (d.compression_mode) {
        setActiveModes(parseModes(String(d.compression_mode)));
      }
      setInjectionLoaded(true);
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

  const toggleMode = async (modeId: string) => {
    const newModes = { ...activeModes };
    if (newModes[modeId]) {
      delete newModes[modeId];
    } else {
      newModes[modeId] = "full"; // default level
    }
    setActiveModes(newModes);
    await api.updateSettings({
      compression_mode: serializeModes(newModes),
    });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const selectLevel = async (modeId: string, level: Level) => {
    const newModes = { ...activeModes, [modeId]: level };
    setActiveModes(newModes);
    await api.updateSettings({
      compression_mode: serializeModes(newModes),
    });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
  };

  const toggleHeadroom = async () => {
    await api.updateSettings({ headroom_enabled: headroomEnabled ? "false" : "true" });
    await queryClient.invalidateQueries({ queryKey: ["settings"] });
    queryClient.invalidateQueries({ queryKey: ["headroom-status"] });
  };

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

        {/* COMPRESSION MODE */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-4">
            <Zap className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium">Compression Mode</span>
          </div>

          <div className="space-y-3">
            {COMPRESSION_MODES.map((mode) => {
              const isActive = mode.id in activeModes;
              const currentLevel = activeModes[mode.id] || "full";

              return (
                <div
                  key={mode.id}
                  className={cn(
                    "border rounded-lg p-3 transition-all",
                    !mode.available && "opacity-40",
                    isActive ? "border-primary/40 bg-primary/5" : "border-border"
                  )}
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{mode.label}</span>
                    </div>
                    {mode.available && (
                      <button
                        onClick={() => toggleMode(mode.id)}
                        className={cn(
                          "w-9 h-5 rounded-full relative transition-colors",
                          isActive ? "bg-green-500" : "bg-foreground/15"
                        )}
                      >
                        <span
                          className={cn(
                            "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
                            isActive ? "left-[18px]" : "left-[2px]"
                          )}
                        />
                      </button>
                    )}
                  </div>

                  <p className="text-[11px] text-muted-foreground mb-2">
                    {mode.description}
                  </p>

                  {mode.available && isActive && (
                    <div className="flex gap-1.5 mt-2">
                      {LEVELS.map((level) => (
                        <button
                          key={level}
                          onClick={() => selectLevel(mode.id, level)}
                          className={cn(
                            "px-2.5 py-1 text-[10px] font-medium rounded border transition-colors capitalize",
                            currentLevel === level
                              ? "border-primary/40 bg-primary/10 text-primary"
                              : "border-border text-muted-foreground hover:text-foreground"
                          )}
                        >
                          {level}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>

        {/* HEADROOM */}
        <div className="bg-primary/[0.04] border border-primary/15 rounded-lg p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <Gauge className="w-4 h-4 text-muted-foreground" />
              <span className="text-sm font-medium">Headroom</span>
            </div>
            <button
              onClick={toggleHeadroom}
              aria-label="Aktifkan Headroom"
              className={cn(
                "w-9 h-5 rounded-full relative transition-colors",
                headroomEnabled ? "bg-green-500" : "bg-foreground/15"
              )}
            >
              <span
                className={cn(
                  "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white ring-1 ring-black/10",
                  headroomEnabled ? "left-[18px]" : "left-[2px]"
                )}
              />
            </button>
          </div>

          <p className="text-[11px] text-muted-foreground">
            Kompresi tool output JSON lewat proxy Headroom, jalan setelah RTK. Butuh service
            Python terpisah. Tool output kecil di-skip otomatis.
          </p>

          {headroomEnabled && headroomQuery.data && (
            <div className="mt-3">
              {headroomQuery.data.reachable ? (
                <span className="inline-flex items-center gap-1.5 text-[11px] text-green-600">
                  <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
                  Proxy aktif di {headroomQuery.data.url}
                </span>
              ) : (
                <div className="space-y-1.5">
                  <span className="inline-flex items-center gap-1.5 text-[11px] text-amber-600">
                    <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                    {headroomQuery.data.hint}
                  </span>
                  <div className="flex items-center gap-2">
                    <pre className="flex-1 px-2.5 py-1.5 text-[11px] rounded border border-input bg-muted/50 font-mono overflow-x-auto">
                      {headroomQuery.data.command}
                    </pre>
                    <button
                      onClick={() => navigator.clipboard.writeText(headroomQuery.data?.command || "")}
                      className="shrink-0 inline-flex items-center justify-center w-7 h-7 rounded border border-border bg-background hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                      title="Copy command"
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v3"/></svg>
                    </button>
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    Jalankan di terminal. PAAP mendeteksi sendiri dalam ~30 detik, tanpa restart.
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title={t("compression_docs_title")}
        sections={[
          { title: t("compression_docs_overview_title"), content: t("compression_docs_overview_content") },
          { title: t("compression_docs_modes_title"), content: t("compression_docs_modes_content") },
          { title: t("compression_docs_injection_title"), content: t("compression_docs_injection_content") },
          { title: t("compression_docs_headroom_title"), content: t("compression_docs_headroom_content") },
          { title: t("compression_docs_voice_title"), content: t("compression_docs_voice_content") },
          { title: t("compression_docs_troubleshoot_title"), content: t("compression_docs_troubleshoot_content") },
        ]}
      />
    </div>
  );
}
