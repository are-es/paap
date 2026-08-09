"use client";

import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import Image from "next/image";
import type { Provider } from "@/lib/api";
import { api } from "@/lib/api";
import { getProviderLogo } from "@/lib/provider-logos";
import { useQuery } from "@tanstack/react-query";

interface ProviderTopologyProps {
  providers: Provider[];
}

const NEON_CYAN = "#22d3ee";
const NEON_AMBER = "#fbbf24";
const NEON_GREEN = "#34d399";

function providerColor(provider: Provider): string {
  const name = provider.name.toLowerCase();
  if (name.includes("google")) return "#4285f4";
  if (name.includes("xiaomi") || name.includes("mimo")) return "#ff6900";
  if (name.includes("kimchi")) return "#e74c3c";
  if (name.includes("meta")) return "#0668e1";
  if (name.includes("openrouter")) return "#6366f1";
  if (name.includes("grok")) return "#f59e0b";
  if (name.includes("anigravity")) return "#f59e0b";
  if (name.includes("ollama")) return "#10b981";
  if (name.includes("deepseek")) return "#0ea5e9";
  if (name.includes("cloudflare")) return "#f48120";
  if (name.includes("runapi")) return "#8b5cf6";
  return "#71717a";
}

interface ModelItem {
  id: string;
  name: string;
  selected?: boolean;
}

interface Pos { x: number; y: number }

// Smart model name abbreviation for topology display (max ~12 chars)
function abbreviateModel(name: string): string {
  let s = name;
  // Strip provider prefix (e.g. "xiaomi/", "nvidia/")
  const slash = s.indexOf("/");
  if (slash >= 0) s = s.slice(slash + 1);
  // Strip context/effort suffixes: [1m], [4m], :free, etc.
  s = s.replace(/\[.*?\]/g, "").replace(/:free$/i, "").replace(/:paid$/i, "");
  // Common abbreviations
  s = s.replace(/^deepseek-/i, "ds-").replace(/^DeepSeek/i, "ds");
  s = s.replace(/^gemini-/i, "gm-");
  s = s.replace(/^claude-/i, "cl-");
  s = s.replace(/^mistral-/i, "mist-");
  s = s.replace(/^meta-llama[-\/]?/i, "");
  s = s.replace(/^qwen/i, "qw");
  s = s.replace(/^nemotron-/i, "nemo-");
  s = s.replace(/^MiniMax-/i, "mm-");
  s = s.replace(/^step-/i, "st-");
  // Trailing descriptor abbreviations
  s = s.replace(/-thinking$/i, "-thk");
  s = s.replace(/-flash$/i, "-fl");
  s = s.replace(/-omni$/i, "-om");
  // If still long, truncate middle
  if (s.length > 13) {
    s = s.slice(0, 7) + "…" + s.slice(-4);
  }
  return s;
}

const LS_KEY = "paap-topo-positions";

export function ProviderTopology({ providers }: ProviderTopologyProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [activeProviders, setActiveProviders] = useState<Set<string>>(new Set());
  const [activeModels, setActiveModels] = useState<Set<string>>(new Set());
  const [tooltip, setTooltip] = useState<{ name: string; info: string; x: number; y: number } | null>(null);
  const [dims, setDims] = useState({ w: 700, h: 520 });

  // Drag state
  const [positions, setPositions] = useState<Record<string, Pos>>(() => {
    // ponytail: load from localStorage on init
    try {
      const raw = localStorage.getItem(LS_KEY);
      return raw ? JSON.parse(raw) : {};
    } catch { return {}; }
  });
  const dragging = useRef<string | null>(null);
  const dragOffset = useRef({ x: 0, y: 0 });

  const allModelsQuery = useQuery({
    queryKey: ["all-models-topology"],
    queryFn: () => api.getAllModels(),
    refetchInterval: 30_000,
  });

  const modelsByProvider = useMemo(() => {
    const map: Record<string, typeof allModelsQuery.data> = {};
    const models = allModelsQuery.data ?? [];
    for (const m of models) {
      const pid = String(m.provider_id);
      if (!map[pid]) map[pid] = [];
      map[pid].push(m);
    }
    return map;
  }, [allModelsQuery.data]);

  // Poll logs for active providers AND active model names
  useEffect(() => {
    const poll = async () => {
      try {
        const res = await fetch("/api/logs?limit=20");
        if (!res.ok) return;
        const data = await res.json();
        const logs = data.data || data.logs || [];
        const now = Date.now();
        const recentProviders = new Set<string>();
        const recentModels = new Set<string>();
        for (const log of logs) {
          const ts = new Date(log.timestamp).getTime();
          if (now - ts < 6000) {
            if (log.provider_name) recentProviders.add(log.provider_name);
            if (log.model_id) recentModels.add(log.model_id);
          }
        }
        if (recentProviders.size > 0 || recentModels.size > 0) {
          setActiveProviders(recentProviders);
          setActiveModels(recentModels);
          setTimeout(() => { setActiveProviders(new Set()); setActiveModels(new Set()); }, 5000);
        }
      } catch {}
    };
    poll();
    const interval = setInterval(poll, 3000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ w: width, h: Math.max(520, height) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const onlineProviders = providers.filter((p) => p.status !== "offline");
  const cx = dims.w / 2;
  const baseGatewayY = 40;
  const baseProviderY = 220;
  const baseModelY = 420;
  const hasActive = activeProviders.size > 0;

  // Calculate default positions (used when no drag has occurred)
  const provPositions = useMemo(() => {
    const count = onlineProviders.length;
    if (count === 0) return [];
    const spacing = Math.min(160, (dims.w - 80) / count);
    const startX = cx - ((count - 1) * spacing) / 2;
    return onlineProviders.map((_, i) => startX + i * spacing);
  }, [onlineProviders.length, cx, dims.w]);

  // Default positions for all nodes
  const defaultPos = useCallback((key: string): Pos => {
    if (key === "gateway") return { x: cx, y: baseGatewayY + 20 };
    // Provider positions
    const provIdx = onlineProviders.findIndex((p) => `prov-${p.id}` === key);
    if (provIdx >= 0) return { x: provPositions[provIdx], y: baseProviderY + 24 };
    // Model positions
    for (let pi = 0; pi < onlineProviders.length; pi++) {
      const prov = onlineProviders[pi];
      const models = modelsByProvider[String(prov.id)] ?? [];
      const px = provPositions[pi];
      if (!px) continue;
      const modelSpacing = Math.min(90, (dims.w - 120) / Math.max(models.length, 1));
      const modelsStartX = px - ((models.length - 1) * modelSpacing) / 2;
      for (let mi = 0; mi < models.length; mi++) {
        const mk = `model-${prov.id}-${models[mi].model_id}`;
        if (mk === key) return { x: modelsStartX + mi * modelSpacing, y: baseModelY + 16 };
      }
    }
    return { x: cx, y: baseGatewayY + 20 };
  }, [cx, onlineProviders, provPositions, modelsByProvider, dims.w]);

  // Get current position (dragged or default)
  const getPos = useCallback((key: string): Pos => {
    return positions[key] ?? defaultPos(key);
  }, [positions, defaultPos]);

  // Mouse handlers for drag
  const handleMouseDown = useCallback((e: React.MouseEvent, key: string) => {
    e.preventDefault();
    e.stopPropagation();
    const pos = getPos(key);
    dragging.current = key;
    dragOffset.current = { x: e.clientX - pos.x, y: e.clientY - pos.y };
  }, [getPos]);

  const handleMouseMove = useCallback((e: React.MouseEvent) => {
    if (!dragging.current || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    const x = e.clientX - dragOffset.current.x;
    const y = e.clientY - dragOffset.current.y;
    // Clamp within container
    const cx = Math.max(20, Math.min(rect.width - 20, x));
    const cy = Math.max(20, Math.min(rect.height - 20, y));
    setPositions((prev) => ({ ...prev, [dragging.current!]: { x: cx, y: cy } }));
  }, []);

  const handleMouseUp = useCallback(() => {
    dragging.current = null;
    // ponytail: persist positions on drag end
    try { localStorage.setItem(LS_KEY, JSON.stringify(positions)); } catch {}
  }, [positions]);

  // Build SVG paths dynamically from current positions
  const gwPos = getPos("gateway");

  return (
    <section className="mb-7" aria-label="Live Activity">
      <div className="flex items-center justify-between mb-4">
        <h2 className="font-heading text-lg font-bold flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-neon-cyan animate-pulse" />
          Live Activity
        </h2>
        <div className="flex items-center gap-4 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: NEON_CYAN }} /> Gateway
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: NEON_AMBER }} /> Provider
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: NEON_GREEN }} /> Model
          </span>
        </div>
      </div>

      <div
        ref={containerRef}
        className="rounded-2xl border border-border min-h-[520px] relative overflow-hidden select-none"
        style={{
          background: "var(--background, #08080f)",
          backgroundImage: "radial-gradient(circle, var(--border, rgba(255,255,255,0.025)) 1px, transparent 1px)",
          backgroundSize: "18px 18px",
          cursor: dragging.current ? "grabbing" : "default",
        }}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseUp}
      >
        {/* Layer tags */}
        <div className="absolute left-4 text-[9px] font-bold tracking-[1.5px] uppercase opacity-40" style={{ top: 28, color: NEON_CYAN }}>Gateway</div>
        <div className="absolute left-4 text-[9px] font-bold tracking-[1.5px] uppercase opacity-40" style={{ top: baseProviderY - 8, color: NEON_AMBER }}>Providers</div>
        <div className="absolute left-4 text-[9px] font-bold tracking-[1.5px] uppercase opacity-40" style={{ top: baseModelY - 8, color: NEON_GREEN }}>Models</div>

        {/* All SVG edges in one layer */}
        <svg className="absolute inset-0 w-full h-full pointer-events-none z-[4]">
          <defs>
            {/* Animated gradient for active edges */}
            <linearGradient id="flowGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={NEON_CYAN} stopOpacity="0.8" />
              <stop offset="100%" stopColor={NEON_CYAN} stopOpacity="0.2" />
            </linearGradient>
          </defs>

          {/* Gateway → Provider paths */}
          {onlineProviders.map((prov, i) => {
            const provPos = getPos(`prov-${prov.id}`);
            const isActive = activeProviders.has(prov.name);
            const color = isActive ? NEON_CYAN : "var(--muted-foreground, #64748b)";
            const opacity = isActive ? 0.55 : 0.15;
            const width = isActive ? 1.8 : 0.8;
            const pathD = `M ${gwPos.x} ${gwPos.y + 28} C ${gwPos.x} ${gwPos.y + 80} ${provPos.x} ${provPos.y - 60} ${provPos.x} ${provPos.y - 4}`;

            return (
              <g key={`gw-${prov.id}`}>
                <path
                  d={pathD}
                  fill="none"
                  stroke={color}
                  strokeWidth={width}
                  strokeDasharray="6,5"
                  strokeLinecap="round"
                  opacity={opacity}
                  style={{ animation: isActive ? "flow-down 1.8s linear infinite" : "none" }}
                />
                {/* Flowing dot */}
                <circle r={3} fill={NEON_CYAN} opacity={isActive ? 0.9 : 0}>
                  <animateMotion dur="1.6s" repeatCount="indefinite" path={pathD} />
                </circle>
                <circle r={1.5} fill="#fff" opacity={isActive ? 0.6 : 0}>
                  <animateMotion dur="1.6s" repeatCount="indefinite" path={pathD} />
                </circle>
              </g>
            );
          })}

          {/* Provider → Model paths */}
          {onlineProviders.map((prov, i) => {
            const provPos = getPos(`prov-${prov.id}`);
            const models = modelsByProvider[String(prov.id)] ?? [];
            const color = providerColor(prov);
            const isProvActive = activeProviders.has(prov.name);

            return models.map((model) => {
              const modelPos = getPos(`model-${prov.id}-${model.model_id}`);
              const isModelActive = activeModels.has(model.model_id);
              const isActive = isProvActive && isModelActive;
              const pathD = `M ${provPos.x} ${provPos.y + 28} C ${provPos.x} ${provPos.y + 60} ${modelPos.x} ${modelPos.y - 40} ${modelPos.x} ${modelPos.y - 4}`;

              return (
                <g key={`pv-${prov.id}-${model.model_id}`}>
                  <path
                    d={pathD}
                    fill="none"
                    stroke={isActive ? color : "var(--muted-foreground, #64748b)"}
                    strokeWidth={isActive ? 1.5 : 0.5}
                    strokeDasharray="3,4"
                    strokeLinecap="round"
                    opacity={isActive ? 0.6 : 0.15}
                    style={{ animation: isActive ? "flow-down 1.2s linear infinite" : "none" }}
                  />
                  {/* Flowing dot — only when active */}
                  {isActive && (
                    <>
                      <circle r={2.5} fill={color} opacity={0.9}>
                        <animateMotion dur="1s" repeatCount="indefinite" path={pathD} />
                      </circle>
                      <circle r={1} fill="#fff" opacity={0.5}>
                        <animateMotion dur="1s" repeatCount="indefinite" path={pathD} />
                      </circle>
                    </>
                  )}
                </g>
              );
            });
          })}
        </svg>

        {/* PAAP Gateway Node (draggable) */}
        <div
          className="absolute z-10 flex flex-col items-center"
          style={{
            top: gwPos.y - 28,
            left: gwPos.x - 45,
            width: 90,
            cursor: dragging.current === "gateway" ? "grabbing" : "grab",
          }}
          onMouseDown={(e) => handleMouseDown(e, "gateway")}
          onMouseEnter={(e) => !dragging.current && setTooltip({ name: "PAAP Gateway", info: "Pangkalan API — drag to move", x: e.clientX, y: e.clientY })}
          onMouseLeave={() => setTooltip(null)}
        >
          <div
            className="w-[80px] h-[56px] rounded-[14px] flex items-center justify-center transition-shadow overflow-hidden"
            style={{
              background: "var(--background, #0f0f1a)",
              border: `2px solid ${hasActive ? "rgba(34,211,238,0.5)" : "rgba(34,211,238,0.3)"}`,
              boxShadow: hasActive
                ? `0 0 28px rgba(34,211,238,0.3), 0 2px 10px rgba(0,0,0,0.4)`
                : "0 0 20px rgba(34,211,238,0.15), 0 2px 10px rgba(0,0,0,0.4)",
            }}
          >
            <Image src="/assets/logo.svg" alt="PAAP" width={48} height={48} className="object-contain" unoptimized />
          </div>
          <span className="text-[10px] font-bold mt-1" style={{ color: NEON_CYAN }}>PAAP</span>
        </div>

        {/* Provider Nodes (draggable) */}
        {onlineProviders.map((prov) => {
          const pos = getPos(`prov-${prov.id}`);
          const isActive = activeProviders.has(prov.name);
          const color = providerColor(prov);
          const logo = getProviderLogo(prov);
          const models = modelsByProvider[String(prov.id)] ?? [];
          return (
            <div
              key={prov.id}
              className="absolute z-10 flex flex-col items-center"
              style={{
                top: pos.y - 24,
                left: pos.x - 40,
                width: 80,
                cursor: dragging.current === `prov-${prov.id}` ? "grabbing" : "grab",
              }}
              onMouseDown={(e) => handleMouseDown(e, `prov-${prov.id}`)}
              onMouseEnter={(e) => !dragging.current && setTooltip({ name: prov.name, info: `${prov.key_count ?? 0} keys · ${models.length} models · ${prov.status}`, x: e.clientX, y: e.clientY })}
              onMouseLeave={() => setTooltip(null)}
            >
              <div
                className="w-[56px] h-[56px] rounded-[14px] flex items-center justify-center overflow-hidden transition-all"
                style={{
                  background: "var(--background, #0f0f1a)",
                  border: `2px solid ${isActive ? color + "80" : color + "30"}`,
                  boxShadow: isActive
                    ? `0 0 20px ${color}50, 0 2px 8px rgba(0,0,0,0.4)`
                    : `0 2px 8px rgba(0,0,0,0.2)`,
                }}
              >
                {logo ? (
                  <Image src={logo} alt="" width={32} height={32} className="rounded-sm object-contain" unoptimized />
                ) : (
                  <span className="text-lg font-bold" style={{ color }}>{prov.name.length >= 2 ? prov.name.slice(0, 2).toUpperCase() : prov.name.charAt(0)}</span>
                )}
              </div>
              <span className="text-[10px] font-semibold mt-1 truncate max-w-[80px] text-center" style={{ color }}>{prov.name}</span>
            </div>
          );
        })}

        {/* Model Cards (draggable) */}
        {onlineProviders.map((prov) => {
          const models = modelsByProvider[String(prov.id)] ?? [];
          if (models.length === 0) return null;
          const color = providerColor(prov);
          const isProvActive = activeProviders.has(prov.name);

          return models.map((model) => {
            const pos = getPos(`model-${prov.id}-${model.model_id}`);
            const isModelActive = activeModels.has(model.model_id);
            const isActive = isProvActive && isModelActive;
            const isSelected = true;
            const shortName = abbreviateModel(model.model_id);

            return (
              <div
                key={`${prov.id}-${model.model_id}`}
                className="absolute z-10 flex flex-col items-center group"
                style={{
                  top: pos.y - 16,
                  left: pos.x - 40,
                  width: 80,
                  cursor: dragging.current === `model-${prov.id}-${model.model_id}` ? "grabbing" : "grab",
                }}
                onMouseDown={(e) => handleMouseDown(e, `model-${prov.id}-${model.model_id}`)}
                onMouseEnter={(e) => !dragging.current && setTooltip({
                  name: model.model_id,
                  info: `${prov.name} · ${isActive ? "active request" : "idle"}`,
                  x: e.clientX,
                  y: e.clientY,
                })}
                onMouseLeave={() => setTooltip(null)}
              >
                <div
                  className="px-2 py-1.5 rounded-lg text-center transition-all min-w-[60px] max-w-[80px]"
                  style={{
                    background: isActive
                      ? `color-mix(in srgb, ${color} 15%, var(--background, #0f0f1a))`
                      : isSelected
                        ? `color-mix(in srgb, ${color} 8%, var(--background, #0f0f1a))`
                        : "var(--background, #0f0f1a)",
                    border: `1.5px solid ${isActive ? color + "80" : isSelected ? color + "40" : "var(--border, rgba(255,255,255,0.06))"}`,
                    boxShadow: isActive
                      ? `0 0 16px ${color}40, 0 0 4px ${color}20`
                      : "none",
                  }}
                >
                  <div className="flex items-center justify-center gap-1 mb-0.5">
                    <span
                      className="w-1.5 h-1.5 rounded-full shrink-0"
                      style={{
                        background: isActive ? NEON_GREEN : isSelected ? NEON_GREEN : "var(--muted-foreground, #64748b)",
                        opacity: isActive ? 1 : isSelected ? 0.7 : 0.4,
                        animation: isActive ? "pulse-green 1s ease-in-out infinite" : "none",
                      }}
                    />
                    <span className="text-[9px] font-mono font-medium truncate" style={{ color: isActive ? "var(--foreground, #e2e8f0)" : "var(--muted-foreground, #94a3b8)" }}>
                      {shortName}
                    </span>
                  </div>
                </div>
              </div>
            );
          });
        })}

        {/* Empty state */}
        {onlineProviders.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center">
            <p className="text-sm text-muted-foreground opacity-60">No online providers</p>
          </div>
        )}

        {/* Tooltip */}
        {tooltip && (
          <div
            className="fixed z-50 pointer-events-none rounded-lg shadow-xl px-3 py-2 text-xs border"
            style={{
              left: tooltip.x + 14,
              top: tooltip.y - 12,
              background: "var(--background, rgba(15, 16, 28, 0.95))",
              borderColor: "var(--border, rgba(255,255,255,0.08))",
              backdropFilter: "blur(12px)",
            }}
          >
            <div className="font-semibold">{tooltip.name}</div>
            <div className="text-muted-foreground mt-0.5">{tooltip.info}</div>
          </div>
        )}
      </div>

      <style jsx global>{`
        @keyframes flow-down { to { stroke-dashoffset: -22; } }
        @keyframes flow-up { to { stroke-dashoffset: 22; } }
        @keyframes pulse-green {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.6; transform: scale(1.3); }
        }
      `}</style>
    </section>
  );
}
