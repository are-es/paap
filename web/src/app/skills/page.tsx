"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type SettingsData } from "@/lib/api";
import { Zap, Save } from "lucide-react";
import { cn } from "@/lib/utils";

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
    id: "ponytail",
    label: "Ponytail",
    description: "Lazy senior dev discipline. YAGNI ladder. Smallest working diff.",
    available: true,
  },
  {
    id: "rtk",
    label: "RTK",
    description: "Rust Token Killer. Compress tool outputs (bash, grep, git, dll) 60-90%.",
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
  const [injectionEnabled, setInjectionEnabled] = useState(false);
  const [injectionText, setInjectionText] = useState("");
  const [injectionPosition, setInjectionPosition] = useState<"prepend" | "append">("prepend");
  const [injectionLoaded, setInjectionLoaded] = useState(false);

  // Compression mode state — per-mode levels
  const [activeModes, setActiveModes] = useState<Record<string, Level>>({});

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });

  if (settingsQuery.data && !injectionLoaded) {
    const d = settingsQuery.data as SettingsData;
    if (d.prompt_injection_enabled !== undefined) {
      setInjectionEnabled(Boolean(d.prompt_injection_enabled));
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

  const saveInjection = async () => {
    await api.updateSettings({
      prompt_injection_enabled: injectionEnabled,
      prompt_injection_text: injectionText,
      prompt_injection_position: injectionPosition,
    });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
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

  return (
    <div className="p-6 md:p-8 min-h-full">
      <h1 className="font-heading text-xl font-bold mb-6">Compression</h1>

      <div className="space-y-4">
        {/* SYSTEM PROMPT */}
        <div className="bg-card border border-border rounded-lg p-4">
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
                onClick={() => setInjectionEnabled(!injectionEnabled)}
                className={cn(
                  "w-9 h-5 rounded-full relative transition-colors",
                  injectionEnabled ? "bg-green-500" : "bg-gray-300 dark:bg-gray-600"
                )}
              >
                <span
                  className={cn(
                    "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white",
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
              onClick={saveInjection}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border border-primary/20 text-primary bg-primary/5 hover:bg-primary/10 transition-colors font-medium"
            >
              <Save className="w-3 h-3" />
              Save
            </button>
          </div>
        </div>

        {/* COMPRESSION MODE */}
        <div className="bg-card border border-border rounded-lg p-4">
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
                          isActive ? "bg-green-500" : "bg-gray-300 dark:bg-gray-600"
                        )}
                      >
                        <span
                          className={cn(
                            "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white",
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
      </div>
    </div>
  );
}
