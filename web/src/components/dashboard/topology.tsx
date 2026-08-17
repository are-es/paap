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

const GW_R = 46;
const PROV_W = 126;
const PROV_H = 42;

// ─── Provider Logo + Name Card ──────────────────────────────
function ProviderCard({
  logo,
  color,
  name,
  isActive,
}: {
  logo: string | null;
  color: string;
  name: string;
  isActive: boolean;
}) {
  const [imgError, setImgError] = useState(false);
  const showLogo = logo && !imgError;

  return (
    <div
      data-prov-card
      className="relative w-[126px] h-[42px] rounded-[10px] flex items-center gap-2.5 px-2.5 select-none transition-all duration-150"
      style={{
        background: "var(--card, #0c0e18)",
        border: `1.5px solid ${isActive ? color : "var(--border, rgba(255,255,255,0.09))"}`,
        boxShadow: isActive
          ? `0 0 20px ${color}45, 0 0 6px ${color}80, 0 2px 8px rgba(0,0,0,0.4)`
          : "0 2px 8px rgba(0,0,0,0.25)",
        transformOrigin: "center center",
      }}
    >
      {showLogo ? (
        <Image
          src={logo}
          alt=""
          width={22}
          height={22}
          className="rounded-md object-contain shrink-0"
          unoptimized
          onError={() => setImgError(true)}
        />
      ) : (
        <div
          className="w-5 h-5 rounded-md shrink-0 flex items-center justify-center font-mono text-[9px] font-black text-white shadow-sm"
          style={{ background: color }}
        >
          {name.slice(0, 2).toUpperCase()}
        </div>
      )}

      <div className="flex flex-col min-w-0 flex-1 justify-center">
        <span className="text-[11px] font-semibold text-foreground truncate leading-tight">
          {name}
        </span>
      </div>

      {/* ECG Status Dot */}
      <span
        className="w-1.5 h-1.5 rounded-full shrink-0"
        style={{
          background: isActive ? color : "var(--muted-foreground, #52525b)",
          boxShadow: isActive ? `0 0 8px ${color}` : "none",
        }}
      />
    </div>
  );
}

function providerColor(provider: Provider): string {
  const name = provider.name.toLowerCase();
  if (name.includes("google")) return "#4285f4";
  if (name.includes("xiaomi") || name.includes("mimo")) return "#ff6900";
  if (name.includes("kimchi")) return "#e74c3c";
  if (name.includes("meta") || name.includes("llama")) return "#0668e1";
  if (name.includes("openrouter")) return "#6366f1";
  if (name.includes("grok") || name.includes("xai")) return "#f59e0b";
  if (name.includes("anigravity")) return "#f59e0b";
  if (name.includes("ollama")) return "#10b981";
  if (name.includes("deepseek")) return "#0ea5e9";
  if (name.includes("cloudflare")) return "#f48120";
  if (name.includes("runapi")) return "#8b5cf6";
  if (name.includes("anthropic")) return "#d97706";
  if (name.includes("openai")) return "#10a37f";
  if (name.includes("mistral")) return "#f97316";
  if (name.includes("together")) return "#ec4899";
  if (name.includes("perplexity")) return "#06b6d4";
  return "#71717a";
}

interface Pos {
  x: number;
  y: number;
}

// Clinical Cardiac Gaussian Deflection (P-Q-R-S-T)
function getEcgDeflection(u: number): number {
  if (u < 0 || u > 1) return 0;

  // P wave (center 0.16, width 0.04, amp +6px)
  const p = 6 * Math.exp(-Math.pow((u - 0.16) / 0.04, 2));

  // Q dip (center 0.38, width 0.018, amp -8px)
  const q = -8 * Math.exp(-Math.pow((u - 0.38) / 0.018, 2));

  // R spike (center 0.46, width 0.016, amp +38px) -> Sharp cardiac spike!
  const r = 38 * Math.exp(-Math.pow((u - 0.46) / 0.016, 2));

  // S dip (center 0.53, width 0.02, amp -14px)
  const s = -14 * Math.exp(-Math.pow((u - 0.53) / 0.02, 2));

  // T wave (center 0.76, width 0.08, amp +12px) -> Broad cardiac repolarization
  const t = 12 * Math.exp(-Math.pow((u - 0.76) / 0.08, 2));

  return p + q + r + s + t;
}

// ─── Main Component ─────────────────────────────────────────
export function ProviderTopology({ providers }: ProviderTopologyProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [activeProviders, setActiveProviders] = useState<Set<string>>(new Set());
  const [activeModels, setActiveModels] = useState<Set<string>>(new Set());
  const [tooltip, setTooltip] = useState<{ name: string; info: string; x: number; y: number } | null>(null);
  const [dims, setDims] = useState({ w: 800, h: 480 });
  const animRaf = useRef<number | null>(null);

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
          setTimeout(() => {
            setActiveProviders(new Set());
            setActiveModels(new Set());
          }, 5000);
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
      setDims({ w: width, h: Math.max(380, height) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const onlineProviders = providers.filter((p) => p.status !== "offline");
  const cx = dims.w / 2;
  const cy = dims.h / 2;
  const hasActive = activeProviders.size > 0;

  // ─── FIXED DUAL-WING SYMMETRICAL LAYOUT ───────────────────
  const wingOffsetX = Math.min(dims.w * 0.36, 360);
  const wingSpreadY = Math.min(dims.h * 0.42, 240);

  const getPos = useCallback(
    (key: string): Pos => {
      if (key === "gateway") return { x: cx, y: cy };

      const provIdx = onlineProviders.findIndex((p) => `prov-${p.id}` === key);
      if (provIdx < 0) return { x: cx, y: cy };

      const total = onlineProviders.length;
      const half = Math.ceil(total / 2);
      const isLeft = provIdx < half;
      const idxInWing = isLeft ? provIdx : provIdx - half;
      const countInWing = isLeft ? half : total - half;

      const step = countInWing > 1 ? idxInWing / (countInWing - 1) - 0.5 : 0;
      const curve = Math.cos(step * Math.PI) * 40;

      return {
        x: isLeft ? cx - wingOffsetX - curve : cx + wingOffsetX + curve,
        y: cy + step * wingSpreadY * 2,
      };
    },
    [cx, cy, wingOffsetX, wingSpreadY, onlineProviders]
  );

  const gwPos = { x: cx, y: cy };

  // ─── S-Curve & Edge Calculation ───────────────────────────
  const getBezierEndpoints = useCallback((pvX: number, pvY: number, isLeft: boolean) => {
    const angle = Math.atan2(pvY - cy, pvX - cx);
    const x1 = cx + Math.cos(angle) * GW_R;
    const y1 = cy + Math.sin(angle) * GW_R;

    // Connect cleanly to inner vertical side facing Gateway
    const x2 = isLeft ? pvX + PROV_W / 2 : pvX - PROV_W / 2;
    const y2 = pvY;

    const dx = x2 - x1;
    const cp1x = x1 + dx * 0.45;
    const cp1y = y1;
    const cp2x = x2 - dx * 0.35;
    const cp2y = y2;

    return { x1, y1, cp1x, cp1y, cp2x, cp2y, x2, y2 };
  }, [cx, cy]);

  // Generate Single Morphing Path (Flatline if idle, ECG pulse if active)
  const generatePath = useCallback((pts: ReturnType<typeof getBezierEndpoints>, cardiacPhase: number, isActive: boolean, isLeft: boolean) => {
    const segs = 140;
    let pathStr = "";

    const isBeating = isActive && cardiacPhase >= 0.15 && cardiacPhase <= 0.75;

    if (!isBeating) {
      for (let i = 0; i <= segs; i++) {
        const s = i / segs;
        const u = 1 - s;
        const bx = u * u * u * pts.x1 + 3 * u * u * s * pts.cp1x + 3 * u * s * s * pts.cp2x + s * s * s * pts.x2;
        const by = u * u * u * pts.y1 + 3 * u * u * s * pts.cp1y + 3 * u * s * s * pts.cp2y + s * s * s * pts.y2;
        if (i === 0) pathStr += `M ${bx} ${by}`;
        else pathStr += ` L ${bx} ${by}`;
      }
      return pathStr;
    }

    const beatProgress = (cardiacPhase - 0.15) / 0.60;
    const pulseWidth = 0.45;
    const pulseStart = beatProgress - pulseWidth / 2;
    const beatAmplitude = Math.sin(Math.PI * beatProgress);

    for (let i = 0; i <= segs; i++) {
      const s = i / segs;
      const u = 1 - s;

      const bx = u * u * u * pts.x1 + 3 * u * u * s * pts.cp1x + 3 * u * s * s * pts.cp2x + s * s * s * pts.x2;
      const by = u * u * u * pts.y1 + 3 * u * u * s * pts.cp1y + 3 * u * s * s * pts.cp2y + s * s * s * pts.y2;

      const tx = 3 * u * u * (pts.cp1x - pts.x1) + 6 * u * s * (pts.cp2x - pts.cp1x) + 3 * s * s * (pts.x2 - pts.cp2x);
      const ty = 3 * u * u * (pts.cp1y - pts.y1) + 6 * u * s * (pts.cp2y - pts.cp1y) + 3 * s * s * (pts.y2 - pts.cp2y);
      const tLen = Math.sqrt(tx * tx + ty * ty) || 1;

      const nx = -ty / tLen;
      const ny = tx / tLen;

      const sideDir = isLeft ? -1 : 1;
      const localU = (s - pulseStart) / pulseWidth;
      const rawDeflection = getEcgDeflection(localU);
      const deflection = rawDeflection * beatAmplitude * sideDir;

      const px = bx + nx * deflection;
      const py = by + ny * deflection;

      if (i === 0) pathStr += `M ${px} ${py}`;
      else pathStr += ` L ${px} ${py}`;
    }

    return pathStr;
  }, []);

  // ─── 60fps Rhythmic ECG Cardiac Render Loop ───────────────
  useEffect(() => {
    const cycleMs = 850; // ~70 BPM natural cardiac cycle

    function tick() {
      const svg = svgRef.current;
      const container = containerRef.current;
      if (!svg || !container) {
        animRaf.current = requestAnimationFrame(tick);
        return;
      }

      const now = Date.now();
      const cardiacPhase = (now % cycleMs) / cycleMs;

      // Gateway beat contraction
      const gwCircleEl = container.querySelector("[data-gw-circle]") as HTMLElement | null;
      if (gwCircleEl) {
        if (hasActive && cardiacPhase >= 0.18 && cardiacPhase <= 0.38) {
          const beatNorm = (cardiacPhase - 0.18) / 0.20;
          const scale = 1 + Math.sin(Math.PI * beatNorm) * 0.07;
          gwCircleEl.style.transform = `scale(${scale})`;
          gwCircleEl.style.boxShadow = `0 0 36px rgba(16,185,129,0.7), 0 0 16px rgba(56,189,248,0.8)`;
        } else {
          gwCircleEl.style.transform = "scale(1)";
          gwCircleEl.style.boxShadow = hasActive
            ? "0 0 24px rgba(16,185,129,0.25)"
            : "0 2px 10px rgba(0,0,0,0.3)";
        }
      }

      // Update SVG path geometries
      onlineProviders.forEach((prov, provIdx) => {
        const half = Math.ceil(onlineProviders.length / 2);
        const isLeft = provIdx < half;
        const provPos = getPos(`prov-${prov.id}`);
        const isActive = activeProviders.has(prov.name);
        const color = providerColor(prov);

        const pts = getBezierEndpoints(provPos.x, provPos.y, isLeft);
        const d = generatePath(pts, cardiacPhase, isActive, isLeft);

        const pathEl = svg.querySelector(`path[data-edge-path="${prov.id}"]`) as SVGPathElement | null;
        const glowEl = svg.querySelector(`path[data-edge-glow="${prov.id}"]`) as SVGPathElement | null;
        const coreEl = svg.querySelector(`path[data-edge-core="${prov.id}"]`) as SVGPathElement | null;

        if (pathEl) pathEl.setAttribute("d", d);
        if (glowEl) {
          glowEl.setAttribute("d", d);
          glowEl.setAttribute("opacity", isActive ? "0.3" : "0");
        }
        if (coreEl) {
          coreEl.setAttribute("d", d);
          coreEl.setAttribute("opacity", isActive ? "0.95" : "0");
        }

        // Active provider card pulse
        const cardEl = container.querySelector(`[data-prov-id="${prov.id}"] [data-prov-card]`) as HTMLElement | null;
        if (cardEl && isActive) {
          if (cardiacPhase >= 0.45 && cardiacPhase <= 0.70) {
            const beatNorm = (cardiacPhase - 0.45) / 0.25;
            const scale = 1 + Math.sin(Math.PI * beatNorm) * 0.04;
            cardEl.style.transform = `scale(${scale})`;
          } else {
            cardEl.style.transform = "scale(1)";
          }
        }
      });

      animRaf.current = requestAnimationFrame(tick);
    }

    animRaf.current = requestAnimationFrame(tick);
    return () => {
      if (animRaf.current) cancelAnimationFrame(animRaf.current);
    };
  }, [hasActive, onlineProviders, getPos, getBezierEndpoints, generatePath, activeProviders]);

  return (
    <section className="mb-7" aria-label="Live Activity">
      <div className="flex items-center justify-between mb-4">
        <h2 className="font-heading text-lg font-bold flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${hasActive ? "bg-emerald-400 animate-pulse" : "bg-zinc-600"}`} />
          Live Activity Topology
        </h2>
      </div>

      <div
        ref={containerRef}
        className="rounded-2xl border border-border relative select-none overflow-hidden shadow-sm"
        style={{
          background: "var(--card, #090a10)",
          backgroundImage:
            "radial-gradient(circle, var(--border, rgba(255,255,255,0.035)) 1px, transparent 1px)",
          backgroundSize: "22px 22px",
          minHeight: 380,
          height: "calc(100vh - 380px)",
          maxHeight: 720,
        }}
      >
        {/* SVG Cable Layer (Single Path Morphing) */}
        <svg
          ref={svgRef}
          className="absolute inset-0 w-full h-full pointer-events-none z-[4]"
          style={{ overflow: "visible" }}
        >
          {onlineProviders.map((prov, provIdx) => {
            const half = Math.ceil(onlineProviders.length / 2);
            const isLeft = provIdx < half;
            const provPos = getPos(`prov-${prov.id}`);
            const isActive = activeProviders.has(prov.name);
            const color = providerColor(prov);
            const pts = getBezierEndpoints(provPos.x, provPos.y, isLeft);

            return (
              <g key={`edge-${prov.id}`} data-prov-id={prov.id}>
                {/* 1. Outer Glow Layer (Active only) */}
                <path
                  data-edge-glow={prov.id}
                  fill="none"
                  stroke={color}
                  strokeWidth="6"
                  strokeLinecap="round"
                  opacity={isActive ? 0.3 : 0}
                  style={{ filter: "blur(4px)" }}
                />

                {/* 2. Main Cable Trace (Morphs from Flatline to ECG Wave) */}
                <path
                  data-edge-path={prov.id}
                  fill="none"
                  stroke={isActive ? color : "var(--border, #25293d)"}
                  strokeWidth={isActive ? "2.6" : "1.8"}
                  strokeLinecap="round"
                  opacity={isActive ? 0.95 : 0.85}
                  style={isActive ? { filter: `drop-shadow(0 0 6px ${color})` } : undefined}
                />

                {/* 3. White Superconducting Core (Active only) */}
                <path
                  data-edge-core={prov.id}
                  fill="none"
                  stroke="#ffffff"
                  strokeWidth="1.1"
                  strokeLinecap="round"
                  opacity={isActive ? 0.95 : 0}
                />

                {/* Port Anchors */}
                <circle
                  cx={pts.x1}
                  cy={pts.y1}
                  r={isActive ? "3.5" : "2.5"}
                  fill={isActive ? color : "var(--border, #3f4561)"}
                  stroke="var(--background, #07080d)"
                  strokeWidth="1"
                />
                <circle
                  cx={pts.x2}
                  cy={pts.y2}
                  r={isActive ? "3.5" : "2.5"}
                  fill={isActive ? color : "var(--border, #3f4561)"}
                  stroke="var(--background, #07080d)"
                  strokeWidth="1"
                />
              </g>
            );
          })}
        </svg>

        {/* PAAP Gateway Node: Center Ring */}
        <div
          className="absolute z-10 -translate-x-1/2 -translate-y-1/2 pointer-events-none"
          style={{
            top: gwPos.y,
            left: gwPos.x,
            width: GW_R * 2,
            height: GW_R * 2,
          }}
        >
          <div
            data-gw-circle
            className="w-full h-full rounded-full flex flex-col items-center justify-center relative overflow-hidden transition-all duration-150"
            style={{
              background: "var(--card, #0d101a)",
              border: `2px solid ${hasActive ? "rgba(16,185,129,0.6)" : "var(--border, rgba(255,255,255,0.15))"}`,
              boxShadow: hasActive
                ? "0 0 28px rgba(16,185,129,0.3)"
                : "0 2px 10px rgba(0,0,0,0.3)",
            }}
          >
            <Image
              src="/assets/logo.svg"
              alt="PAAP"
              width={50}
              height={50}
              className="object-contain shrink-0"
              unoptimized
            />
          </div>
        </div>

        {/* Provider Nodes */}
        {onlineProviders.map((prov, provIdx) => {
          const provPos = getPos(`prov-${prov.id}`);
          const isActive = activeProviders.has(prov.name);
          const color = providerColor(prov);
          const logo = getProviderLogo(prov);
          const models = modelsByProvider[String(prov.id)] ?? [];
          const activeModelCount = models.filter((m) => activeModels.has(m.model_id)).length;

          return (
            <div
              key={prov.id}
              data-prov-id={prov.id}
              className="absolute z-10 -translate-x-1/2 -translate-y-1/2"
              style={{
                top: provPos.y,
                left: provPos.x,
              }}
              onMouseEnter={(e) =>
                setTooltip({
                  name: prov.name,
                  info: `${prov.key_count ?? 0} keys · ${models.length} models${
                    activeModelCount > 0 ? ` · ${activeModelCount} active` : ""
                  } · ${prov.status}`,
                  x: e.clientX,
                  y: e.clientY,
                })
              }
              onMouseLeave={() => setTooltip(null)}
            >
              <ProviderCard logo={logo} color={color} name={prov.name} isActive={isActive} />
            </div>
          );
        })}

        {/* Empty State */}
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
              background: "var(--card, rgba(15, 16, 28, 0.95))",
              borderColor: "var(--border, rgba(255,255,255,0.1))",
              backdropFilter: "blur(12px)",
            }}
          >
            <div className="font-semibold text-foreground">{tooltip.name}</div>
            <div className="text-muted-foreground mt-0.5">{tooltip.info}</div>
          </div>
        )}
      </div>
    </section>
  );
}
