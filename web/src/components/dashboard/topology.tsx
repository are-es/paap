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
const GW_R = 48;
const PROV_W = 110;
const PROV_H = 40;

// ─── Provider Logo + Name Card ──────────────────────────────
function ProviderCard({ logo, color, name, isActive }: { logo: string | null; color: string; name: string; isActive: boolean }) {
  const [imgError, setImgError] = useState(false);
  const showLogo = logo && !imgError;
  return (
    <div
      data-prov-card
      className="w-[110px] h-[40px] rounded-[10px] flex items-center gap-2 px-2.5 transition-all"
      style={{
        background: "#0f0f1a",
        border: `2px solid ${isActive ? color + "80" : color + "30"}`,
        boxShadow: isActive
          ? `0 0 14px ${color}40`
          : "0 2px 8px rgba(0,0,0,0.2)",
        transformOrigin: "center center",
      }}
    >
      {showLogo ? (
        <Image src={logo} alt="" width={24} height={24} className="rounded-md object-contain flex-shrink-0" unoptimized onError={() => setImgError(true)} />
      ) : (
        <div className="w-6 h-6 rounded-md flex-shrink-0" style={{ background: color + "20" }}>
          <span className="flex items-center justify-center w-full h-full text-[10px] font-bold" style={{ color }}>{name.slice(0, 2).toUpperCase()}</span>
        </div>
      )}
      <span className="text-[10px] font-medium text-[#d4d4d8] whitespace-nowrap overflow-hidden text-ellipsis">{name}</span>
    </div>
  );
}

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

interface Pos { x: number; y: number }

const LS_KEY = "paap-topo-positions";

// Border intersection: line from center → target hits card border
function borderPoint(cx: number, cy: number, hw: number, hh: number, tx: number, ty: number) {
  const dx = tx - cx, dy = ty - cy;
  if (dx === 0 && dy === 0) return { x: cx, y: cy + hh };
  const adx = Math.abs(dx), ady = Math.abs(dy);
  if (adx * hh > ady * hw) {
    const sign = dx > 0 ? 1 : -1;
    return { x: cx + sign * hw, y: cy + dy * hw / adx };
  }
  const sign = dy > 0 ? 1 : -1;
  return { x: cx + dx * hh / ady, y: cy + sign * hh };
}

// Curved path: S-curve, arc, or wave
function curvePath(x1: number, y1: number, x2: number, y2: number, style: number, curveFactor = 0.18) {
  const ex = x2 - x1, ey = y2 - y1;
  const dist = Math.sqrt(ex * ex + ey * ey);
  const nx = -ey / (dist || 1), ny = ex / (dist || 1);
  const curve = dist * curveFactor;
  const side = style % 2 === 0 ? 1 : -1;

  if (style === 0) {
    const cp1x = x1 + ex * 0.3 + nx * curve * side;
    const cp1y = y1 + ey * 0.3 + ny * curve * side;
    const cp2x = x1 + ex * 0.7 - nx * curve * side;
    const cp2y = y1 + ey * 0.7 - ny * curve * side;
    return `M ${x1} ${y1} C ${cp1x} ${cp1y} ${cp2x} ${cp2y} ${x2} ${y2}`;
  }
  if (style === 1) {
    const mx = (x1 + x2) / 2, my = (y1 + y2) / 2;
    const cpx = mx + nx * curve * 1.4 * side;
    const cpy = my + ny * curve * 1.4 * side;
    return `M ${x1} ${y1} Q ${cpx} ${cpy} ${x2} ${y2}`;
  }
  const cp1x = x1 + ex * 0.25 + nx * curve * 0.6 * side;
  const cp1y = y1 + ey * 0.25 + ny * curve * 0.6 * side;
  const cp2x = x1 + ex * 0.6 + nx * curve * 0.3 * side;
  const cp2y = y1 + ey * 0.6 + ny * curve * 0.3 * side;
  return `M ${x1} ${y1} C ${cp1x} ${cp1y} ${cp2x} ${cp2y} ${x2} ${y2}`;
}

// ─── Main Component ─────────────────────────────────────────
export function ProviderTopology({ providers }: ProviderTopologyProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [activeProviders, setActiveProviders] = useState<Set<string>>(new Set());
  const [activeModels, setActiveModels] = useState<Set<string>>(new Set());
  const [tooltip, setTooltip] = useState<{ name: string; info: string; x: number; y: number } | null>(null);
  const [dims, setDims] = useState({ w: 700, h: 400 });
  const animRaf = useRef<number | null>(null);

  const [positions, setPositions] = useState<Record<string, Pos>>(() => {
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

  // Poll logs for active providers
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

  // Resize observer
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ w: width, h: Math.max(350, height) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // ─── JS Animation Loop (single raf, dots + card pulse) ─────
  useEffect(() => {
    const CYCLE = 1800; // 1.8s per cycle
    const DOT1_END = 0.40;
    const DOT2_START = 0.50;
    const DOT2_END = 0.90;

    function tick() {
      const svg = svgRef.current;
      const container = containerRef.current;
      if (!svg || !container) { animRaf.current = requestAnimationFrame(tick); return; }

      const t = (Date.now() % CYCLE) / CYCLE;

      // Find all active path groups
      const groups = svg.querySelectorAll("[data-active-group]");
      groups.forEach((g) => {
        const provId = g.getAttribute("data-prov-id");
        const pathEl = g.querySelector("path[data-edge]") as SVGPathElement | null;
        if (!pathEl) return;

        const totalLen = pathEl.getTotalLength();
        const d1 = g.querySelector("[data-dot1]") as SVGCircleElement | null;
        const c1 = g.querySelector("[data-dot1-core]") as SVGCircleElement | null;
        const d2 = g.querySelector("[data-dot2]") as SVGCircleElement | null;
        const c2 = g.querySelector("[data-dot2-core]") as SVGCircleElement | null;

        // Dot 1: gw → provider
        if (d1 && c1) {
          if (t < DOT1_END) {
            const progress = t / DOT1_END;
            const pt = pathEl.getPointAtLength(progress * totalLen);
            d1.setAttribute("cx", String(pt.x));
            d1.setAttribute("cy", String(pt.y));
            c1.setAttribute("cx", String(pt.x));
            c1.setAttribute("cy", String(pt.y));
            d1.setAttribute("opacity", "0.9");
            c1.setAttribute("opacity", "0.7");
          } else {
            d1.setAttribute("opacity", "0");
            c1.setAttribute("opacity", "0");
          }
        }

        // Dot 2: gw → provider (second wave)
        if (d2 && c2) {
          if (t >= DOT2_START && t < DOT2_END) {
            const progress = (t - DOT2_START) / (DOT2_END - DOT2_START);
            const pt = pathEl.getPointAtLength(progress * totalLen);
            d2.setAttribute("cx", String(pt.x));
            d2.setAttribute("cy", String(pt.y));
            c2.setAttribute("cx", String(pt.x));
            c2.setAttribute("cy", String(pt.y));
            d2.setAttribute("opacity", "0.75");
            c2.setAttribute("opacity", "0.5");
          } else {
            d2.setAttribute("opacity", "0");
            c2.setAttribute("opacity", "0");
          }
        }

        // Card pulse when dot arrives
        const cardEl = container.querySelector(`[data-prov-id="${provId}"] [data-prov-card]`) as HTMLElement | null;
        if (cardEl) {
          const pulse1 = t >= 0.37 && t < 0.44;
          const pulse2 = t >= 0.48 && t < 0.55;
          if (pulse1) {
            cardEl.style.transform = `scale(${1 + 0.08 * Math.sin((t - 0.37) / 0.07 * Math.PI)})`;
          } else if (pulse2) {
            cardEl.style.transform = `scale(${1 + 0.06 * Math.sin((t - 0.48) / 0.07 * Math.PI)})`;
          } else {
            cardEl.style.transform = "scale(1)";
          }
        }
      });

      animRaf.current = requestAnimationFrame(tick);
    }

    animRaf.current = requestAnimationFrame(tick);
    return () => { if (animRaf.current) cancelAnimationFrame(animRaf.current); };
  }, []); // runs once — reads DOM each frame

  const onlineProviders = providers.filter((p) => p.status !== "offline");
  const cx = dims.w / 2;
  const cy = dims.h / 2;
  const hasActive = activeProviders.size > 0;

  // ─── DUAL CLUSTER LAYOUT ──────────────────────────────────
  const clusterOffset = Math.min(dims.w * 0.28, 260);
  const clusterRadius = Math.min(dims.h * 0.32, 160);

  const defaultPos = useCallback((key: string): Pos => {
    if (key === "gateway") return { x: cx, y: cy };

    const provIdx = onlineProviders.findIndex((p) => `prov-${p.id}` === key);
    if (provIdx < 0) return { x: cx, y: cy };

    const total = onlineProviders.length;
    const mid = Math.ceil(total / 2);
    const isLeft = provIdx < mid;
    const clusterCx = isLeft ? cx - clusterOffset : cx + clusterOffset;
    const idx = isLeft ? provIdx : provIdx - mid;
    const count = isLeft ? mid : total - mid;

    const angle = (idx / Math.max(count - 1, 1)) * Math.PI * 2 - Math.PI / 2;
    return {
      x: clusterCx + Math.cos(angle) * clusterRadius,
      y: cy + Math.sin(angle) * clusterRadius,
    };
  }, [cx, cy, clusterOffset, clusterRadius, onlineProviders]);

  const getPos = useCallback((key: string): Pos => {
    const raw = positions[key] ?? defaultPos(key);
    const pad = 10;
    const hw = key === "gateway" ? GW_R : PROV_W / 2;
    const hh = key === "gateway" ? GW_R : PROV_H / 2;
    return {
      x: Math.max(pad + hw, Math.min(dims.w - pad - hw, raw.x)),
      y: Math.max(pad + hh, Math.min(dims.h - pad - hh, raw.y)),
    };
  }, [positions, defaultPos, dims.w, dims.h]);

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
    setPositions((prev) => ({
      ...prev,
      [dragging.current!]: {
        x: Math.max(20, Math.min(rect.width - 20, x)),
        y: Math.max(20, Math.min(rect.height - 20, y)),
      },
    }));
  }, []);

  const handleMouseUp = useCallback(() => {
    dragging.current = null;
    try { localStorage.setItem(LS_KEY, JSON.stringify(positions)); } catch {}
  }, [positions]);

  const gwPos = getPos("gateway");

  return (
    <section className="mb-7" aria-label="Live Activity">
      <div className="flex items-center justify-between mb-4">
        <h2 className="font-heading text-lg font-bold flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-neon-cyan animate-pulse" />
          Live Activity
        </h2>
      </div>

      <div
        ref={containerRef}
        className="rounded-2xl border border-border relative select-none overflow-hidden"
        style={{
          background: "var(--background, #08080f)",
          backgroundImage: "radial-gradient(circle, var(--border, rgba(255,255,255,0.025)) 1px, transparent 1px)",
          backgroundSize: "18px 18px",
          cursor: dragging.current ? "grabbing" : "default",
          minHeight: 350,
          height: "calc(100vh - 380px)",
          maxHeight: 700,
        }}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseUp}
      >
        {/* SVG edges layer */}
        <svg ref={svgRef} className="absolute inset-0 w-full h-full pointer-events-none z-[4]" style={{ overflow: "visible" }}>
          {/* Gateway → Provider edges */}
          {onlineProviders.map((prov, provIdx) => {
            const provPos = getPos(`prov-${prov.id}`);
            const isActive = activeProviders.has(prov.name);
            const color = providerColor(prov);

            const gwEdge = borderPoint(gwPos.x, gwPos.y, GW_R, GW_R, provPos.x, provPos.y);
            const pvEdge = borderPoint(provPos.x, provPos.y, PROV_W / 2, PROV_H / 2, gwPos.x, gwPos.y);
            const d = curvePath(gwEdge.x, gwEdge.y, pvEdge.x, pvEdge.y, provIdx % 3);

            return (
              <g key={`edge-${prov.id}`} data-active-group={isActive ? "1" : ""} data-prov-id={prov.id}>
                {/* Idle line (ALWAYS visible) */}
                <path
                  data-edge={isActive ? "active" : "idle"}
                  d={d}
                  fill="none"
                  stroke={isActive ? color : "#b0b8c8"}
                  strokeWidth={isActive ? "2" : "1.2"}
                  opacity={isActive ? 0.35 : 0.5}
                  strokeDasharray="8 4"
                  strokeLinecap="round"
                  style={isActive ? { filter: `drop-shadow(0 0 3px ${color})` } : undefined}
                />
                {/* Dots (only for active — JS drives visibility) */}
                {isActive && (
                  <>
                    <circle data-dot1 r="4.5" fill={color} opacity="0" style={{ filter: `drop-shadow(0 0 5px ${color}) drop-shadow(0 0 10px ${color})` }} />
                    <circle data-dot1-core r="1.8" fill="#fff" opacity="0" />
                    <circle data-dot2 r="3.5" fill={color} opacity="0" style={{ filter: `drop-shadow(0 0 4px ${color}) drop-shadow(0 0 8px ${color})` }} />
                    <circle data-dot2-core r="1.2" fill="#fff" opacity="0" />
                  </>
                )}
              </g>
            );
          })}
        </svg>

        {/* PAAP Gateway Node — Big Circle */}
        <div
          className="absolute z-10"
          style={{
            top: gwPos.y - GW_R,
            left: gwPos.x - GW_R,
            width: GW_R * 2,
            height: GW_R * 2,
            cursor: dragging.current === "gateway" ? "grabbing" : "grab",
          }}
          onMouseDown={(e) => handleMouseDown(e, "gateway")}
          onMouseEnter={(e) => !dragging.current && setTooltip({ name: "PAAP Gateway", info: "Pangkalan API — drag to move", x: e.clientX, y: e.clientY })}
          onMouseLeave={() => setTooltip(null)}
        >
          <div
            className="w-full h-full rounded-full flex items-center justify-center overflow-hidden"
            style={{
              background: "var(--background, #0f0f1a)",
              border: `2px solid ${hasActive ? "rgba(34,211,238,0.5)" : "rgba(34,211,238,0.25)"}`,
              boxShadow: hasActive
                ? "0 0 28px rgba(34,211,238,0.3), 0 2px 10px rgba(0,0,0,0.4)"
                : "0 0 16px rgba(34,211,238,0.1), 0 2px 8px rgba(0,0,0,0.4)",
              animation: hasActive ? "gwHeartbeat 0.7s ease-in-out infinite alternate" : "none",
            }}
          >
            <Image src="/assets/logo.svg" alt="PAAP" width={56} height={56} className="object-contain" unoptimized />
          </div>
        </div>

        {/* Provider Nodes — Logo + Name */}
        {onlineProviders.map((prov) => {
          const pos = getPos(`prov-${prov.id}`);
          const isActive = activeProviders.has(prov.name);
          const color = providerColor(prov);
          const logo = getProviderLogo(prov);
          const models = modelsByProvider[String(prov.id)] ?? [];
          const activeModelCount = models.filter((m) => activeModels.has(m.model_id)).length;
          return (
            <div
              key={prov.id}
              data-prov-id={prov.id}
              className="absolute z-10"
              style={{
                top: pos.y - PROV_H / 2,
                left: pos.x - PROV_W / 2,
                cursor: dragging.current === `prov-${prov.id}` ? "grabbing" : "grab",
              }}
              onMouseDown={(e) => handleMouseDown(e, `prov-${prov.id}`)}
              onMouseEnter={(e) => !dragging.current && setTooltip({
                name: prov.name,
                info: `${prov.key_count ?? 0} keys · ${models.length} models${activeModelCount > 0 ? ` · ${activeModelCount} active` : ""} · ${prov.status}`,
                x: e.clientX,
                y: e.clientY,
              })}
              onMouseLeave={() => setTooltip(null)}
            >
              <ProviderCard logo={logo} color={color} name={prov.name} isActive={isActive} />
            </div>
          );
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
        @keyframes gwHeartbeat {
          0% { box-shadow: 0 0 20px rgba(34,211,238,0.25), 0 2px 8px rgba(0,0,0,0.4); }
          100% { box-shadow: 0 0 32px rgba(34,211,238,0.5), 0 2px 8px rgba(0,0,0,0.4); }
        }
      `}</style>
    </section>
  );
}
