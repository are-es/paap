"use client";

import { useMemo, useState, useRef, useEffect } from "react";
import Image from "next/image";
import type { Provider } from "@/lib/api";
import { getProviderLogo } from "@/lib/provider-logos";

interface ProviderTopologyProps {
  providers: Provider[];
}

interface NodeLayout {
  provider: Provider;
  status: "active" | "idle" | "error";
  x: number;
  y: number;
  ring: number;
  angle: number;
}

interface Star {
  x: number;
  y: number;
  r: number;
  baseAlpha: number;
  speed: number;
  phase: number;
  color: string;
}

interface Nebula {
  x: number;
  y: number;
  r: number;
  color: string;
}

const STAR_COLORS = [
  "#ffffff", "#ffffff", "#ffffff",
  "#e8eaff", "#c8d4ff", "#a8c0ff",
  "#b0f0ff", "#80dfff", "#60d0f0",
  "#ffe8c0", "#ffd090", "#ffc070",
];

const NEON_CYAN = "#22d3ee";
const NEON_MAGENTA = "#e879f9";
const NEON_PURPLE = "#a78bfa";

const ICON_SIZE = 48;
const ICON_SIZE_SM = 40;
const ICON_SIZE_MIN = 22;
const RING_PADDING = 28; // clears the outer ping halo (inset -12) + edge breathing room
const CURVE_OFFSET = 50;

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
  return "#71717a";
}

function getRings(count: number): { r: number; max: number }[] {
  if (count <= 6) return [{ r: 200, max: 6 }];
  if (count <= 12) return [{ r: 160, max: 5 }, { r: 280, max: 7 }];
  return [{ r: 140, max: 5 }, { r: 240, max: 6 }, { r: 340, max: 8 }];
}

function generateStars(w: number, h: number): Star[] {
  const stars: Star[] = [];
  for (let i = 0; i < 200; i++) {
    stars.push({
      x: Math.random() * w,
      y: Math.random() * h,
      r: 0.5 + Math.random() * 2,
      baseAlpha: 0.2 + Math.random() * 0.6,
      speed: 0.5 + Math.random() * 2,
      phase: Math.random() * Math.PI * 2,
      color: STAR_COLORS[Math.floor(Math.random() * STAR_COLORS.length)],
    });
  }
  for (let i = 0; i < 4; i++) {
    stars.push({
      x: Math.random() * w,
      y: Math.random() * h,
      r: 2.5 + Math.random() * 1.5,
      baseAlpha: 0.6 + Math.random() * 0.3,
      speed: 0.3 + Math.random() * 0.5,
      phase: Math.random() * Math.PI * 2,
      color: "#ffffff",
    });
  }
  return stars;
}

function generateNebulae(w: number, h: number): Nebula[] {
  return [
    { x: w * 0.2, y: h * 0.3, r: Math.min(w, h) * 0.25, color: NEON_CYAN },
    { x: w * 0.75, y: h * 0.65, r: Math.min(w, h) * 0.2, color: NEON_MAGENTA },
    { x: w * 0.5, y: h * 0.15, r: Math.min(w, h) * 0.18, color: NEON_PURPLE },
  ];
}

function curvedPath(nx: number, ny: number, cx: number, cy: number, seed: number): string {
  const mx = (nx + cx) / 2;
  const my = (ny + cy) / 2;
  const dx = nx - cx;
  const dy = ny - cy;
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  const perpX = -dy / len;
  const perpY = dx / len;
  const direction = seed % 2 === 0 ? 1 : -1;
  const offset = CURVE_OFFSET * (0.6 + (seed % 5) * 0.1) * direction;
  const cpx = mx + perpX * offset;
  const cpy = my + perpY * offset;
  return `M ${cx} ${cy} Q ${cpx} ${cpy} ${nx} ${ny}`;
}

export function ProviderTopology({ providers }: ProviderTopologyProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const animRef = useRef<number>(0);
  const starsRef = useRef<Star[]>([]);
  const nebulaeRef = useRef<Nebula[]>([]);
  const [activeProviders, setActiveProviders] = useState<Set<string>>(new Set());
  const activeProvidersRef = useRef<Set<string>>(new Set());
  const [tooltip, setTooltip] = useState<{ name: string; keys: number; status: string; x: number; y: number } | null>(null);
  const [dims, setDims] = useState({ w: 900, h: 620 });
  const [tick, setTick] = useState(0);

  useEffect(() => {
    activeProvidersRef.current = activeProviders;
  }, [activeProviders]);

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 50);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const poll = async () => {
      try {
        const res = await fetch("/api/logs?limit=20");
        if (!res.ok) return;
        const data = await res.json();
        const logs = data.data || data.logs || [];
        const now = Date.now();
        const recent = new Set<string>();
        for (const log of logs) {
          const ts = new Date(log.timestamp).getTime();
          if (now - ts < 6000 && log.provider_name) {
            recent.add(log.provider_name);
          }
        }
        if (recent.size > 0) {
          setActiveProviders(recent);
          setTimeout(() => setActiveProviders(new Set()), 5000);
        }
      } catch {}
    };
    poll();
    const interval = setInterval(poll, 2000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDims({ w: width, h: height });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = dims.w * dpr;
    canvas.height = dims.h * dpr;
    canvas.style.width = `${dims.w}px`;
    canvas.style.height = `${dims.h}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    starsRef.current = generateStars(dims.w, dims.h);
    nebulaeRef.current = generateNebulae(dims.w, dims.h);

    let lastTime = 0;
    const interval = 1000 / 30;

    const draw = (time: number) => {
      animRef.current = requestAnimationFrame(draw);
      const delta = time - lastTime;
      if (delta < interval) return;
      lastTime = time - (delta % interval);

      ctx.clearRect(0, 0, dims.w, dims.h);

      const canvasBg = "#0a0715";
      ctx.fillStyle = canvasBg;
      ctx.fillRect(0, 0, dims.w, dims.h);

      for (const neb of nebulaeRef.current) {
        const grad = ctx.createRadialGradient(neb.x, neb.y, 0, neb.x, neb.y, neb.r);
        grad.addColorStop(0, neb.color + "18");
        grad.addColorStop(0.5, neb.color + "08");
        grad.addColorStop(1, "transparent");
        ctx.fillStyle = grad;
        ctx.fillRect(neb.x - neb.r, neb.y - neb.r, neb.r * 2, neb.r * 2);
      }

      const t = time / 1000;
      for (const star of starsRef.current) {
        const alpha = star.baseAlpha * (0.5 + 0.5 * Math.sin(t * star.speed + star.phase));
        ctx.beginPath();
        ctx.arc(star.x, star.y, star.r, 0, Math.PI * 2);
        ctx.fillStyle = star.color;
        ctx.globalAlpha = alpha;
        ctx.fill();

        if (star.r > 2.5) {
          const glow = ctx.createRadialGradient(star.x, star.y, 0, star.x, star.y, star.r * 4);
          glow.addColorStop(0, star.color + "40");
          glow.addColorStop(1, "transparent");
          ctx.fillStyle = glow;
          ctx.globalAlpha = alpha * 0.5;
          ctx.fillRect(star.x - star.r * 4, star.y - star.r * 4, star.r * 8, star.r * 8);
        }
      }
      ctx.globalAlpha = 1;
    };

    animRef.current = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(animRef.current);
  }, [dims.w, dims.h]);

  const onlineProviders = providers.filter((p) => p.status !== "offline");
  const rawRings = useMemo(() => getRings(onlineProviders.length), [onlineProviders.length]);
  const cx = dims.w / 2;
  const cy = dims.h / 2;

  // Scale rings + icons to fit the container so outer-ring nodes never clip.
  // Ring radii from getRings() are nominal; shrink them (and the icons) when
  // the container is smaller than the outermost ring needs.
  const outerR = rawRings[rawRings.length - 1]?.r ?? 200;
  const nominalSize = onlineProviders.length > 10 ? ICON_SIZE_SM : ICON_SIZE;
  const budget = Math.min(dims.w, dims.h) / 2 - nominalSize / 2 - RING_PADDING;
  const scale = Math.min(1, Math.max(0.35, budget / outerR));
  const nodeSize = Math.max(ICON_SIZE_MIN, Math.round(nominalSize * scale));
  const rings = useMemo(
    () => rawRings.map((ring) => ({ ...ring, r: ring.r * scale })),
    [rawRings, scale]
  );

  const nodes = useMemo<NodeLayout[]>(() => {
    let idx = 0;
    const result: NodeLayout[] = [];
    for (const ring of rings) {
      const countInRing = Math.min(ring.max, onlineProviders.length - idx);
      for (let i = 0; i < countInRing && idx < onlineProviders.length; i++, idx++) {
        const p = onlineProviders[idx];
        const angle = (i / countInRing) * Math.PI * 2 - Math.PI / 2;
        result.push({
          provider: p,
          status: activeProviders.has(p.name) ? "active" : p.status === "offline" ? "error" : "idle",
          x: cx + Math.cos(angle) * ring.r,
          y: cy + Math.sin(angle) * ring.r,
          ring: rings.indexOf(ring),
          angle,
        });
      }
    }
    return result;
  }, [onlineProviders, activeProviders, rings, cx, cy]);

  const hasActive = nodes.some((n) => n.status === "active");
  const dashOffset = -(tick * 0.4) % 24;

  return (
    <section className="mb-7" aria-label="Live Orbit">
      <div className="flex items-center justify-between mb-4">
        <h2 className="font-heading text-lg font-bold flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-neon-cyan animate-pulse" />
          Live Orbit
        </h2>
        <div className="flex items-center gap-4 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-neon-cyan" /> Active
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-muted-foreground" /> Idle
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-neon-magenta opacity-40" /> Offline
          </span>
        </div>
      </div>

      <div
        ref={containerRef}
        className="rounded-2xl border border-white/5 min-h-[480px] relative overflow-hidden"
        style={{ background: "#0a0715" }}
      >
        <canvas
          ref={canvasRef}
          className="absolute inset-0 w-full h-full z-0"
          style={{ pointerEvents: "none" }}
        />

        <svg className="absolute inset-0 w-full h-full pointer-events-none z-[1]">
          <defs>
            <linearGradient id="line-active" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor={NEON_CYAN} stopOpacity={0.1} />
              <stop offset="50%" stopColor={NEON_CYAN} stopOpacity={0.8} />
              <stop offset="100%" stopColor={NEON_CYAN} stopOpacity={0.1} />
            </linearGradient>
            <filter id="glow-cyan">
              <feGaussianBlur stdDeviation="3" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
            <filter id="glow-strong">
              <feGaussianBlur stdDeviation="5" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          {rings.map((ring, i) => (
            <circle
              key={`ring-${i}`}
              cx={cx}
              cy={cy}
              r={ring.r}
              fill="none"
              stroke="rgba(255,255,255,0.04)"
              strokeWidth={1}
              strokeDasharray="4,8"
            />
          ))}

          {nodes.map((node, idx) => {
            const isActive = node.status === "active";
            const color = isActive ? NEON_CYAN : providerColor(node.provider);
            const opacity = isActive ? 0.9 : node.status === "error" ? 0.15 : 0.35;
            const pathD = curvedPath(node.x, node.y, cx, cy, idx);
            return (
              <g key={`line-${node.provider.id}`}>
                {isActive && (
                  <path
                    d={pathD}
                    fill="none"
                    stroke={NEON_CYAN}
                    strokeWidth={6}
                    strokeDasharray="4,8"
                    strokeDashoffset={dashOffset}
                    opacity={0.1}
                    filter="url(#glow-strong)"
                  />
                )}
                <path
                  d={pathD}
                  fill="none"
                  stroke={isActive ? NEON_CYAN : color}
                  strokeWidth={isActive ? 1.5 : 0.8}
                  strokeDasharray={isActive ? "4,8" : "6,6"}
                  strokeDashoffset={isActive ? dashOffset : 0}
                  opacity={opacity}
                  style={{ transition: "stroke 0.6s ease, stroke-width 0.6s ease, opacity 0.6s ease" }}
                />
                {isActive && (
                  <>
                    <circle r={3} fill={NEON_CYAN} opacity={0.9} filter="url(#glow-cyan)">
                      <animateMotion
                        dur="1.5s"
                        repeatCount="indefinite"
                        path={pathD}
                      />
                    </circle>
                    <circle r={1.5} fill="#fff" opacity={0.7}>
                      <animateMotion
                        dur="1.5s"
                        repeatCount="indefinite"
                        path={pathD}
                      />
                    </circle>
                  </>
                )}
              </g>
            );
          })}

          {nodes
            .filter((n) => n.status === "active")
            .map((node) => {
              const arcR = rings[node.ring]?.r ?? 200;
              const spread = 0.18;
              const a1 = node.angle - spread;
              const a2 = node.angle + spread;
              const x1 = cx + Math.cos(a1) * arcR;
              const y1 = cy + Math.sin(a1) * arcR;
              const x2 = cx + Math.cos(a2) * arcR;
              const y2 = cy + Math.sin(a2) * arcR;
              return (
                <g key={`arc-${node.provider.id}`}>
                  <path
                    d={`M ${x1} ${y1} A ${arcR} ${arcR} 0 0 1 ${x2} ${y2}`}
                    fill="none"
                    stroke={NEON_CYAN}
                    strokeWidth={3}
                    opacity={0.2}
                    filter="url(#glow-strong)"
                  />
                  <path
                    d={`M ${x1} ${y1} A ${arcR} ${arcR} 0 0 1 ${x2} ${y2}`}
                    fill="none"
                    stroke={NEON_CYAN}
                    strokeWidth={1.5}
                    opacity={0.6}
                  />
                </g>
              );
            })}
        </svg>

        <div
          className="absolute z-10 pointer-events-none"
          style={{ left: cx - 48, top: cy - 28 }}
        >
          <div className="relative">
            <div
              className="absolute inset-0 rounded-full blur-xl opacity-20"
              style={{ background: NEON_CYAN }}
            />
            <Image src="/assets/logo.svg" alt="PAAP" width={96} height={28} className="w-24 h-auto mx-auto relative" unoptimized />
          </div>
          <div className="text-[10px] text-muted-foreground mt-1 font-mono tracking-wider text-center">SMART ROUTER</div>
        </div>

        {nodes.map((node) => {
          const color = providerColor(node.provider);
          const logo = getProviderLogo(node.provider);
          const isActive = node.status === "active";
          const isError = node.status === "error";
          const half = nodeSize / 2;

          return (
            <div
              key={node.provider.id}
              className="absolute cursor-pointer transition-all duration-500"
              style={{
                left: node.x - half,
                top: node.y - half,
                width: nodeSize,
                height: nodeSize,
                opacity: isError ? 0.35 : 1,
                zIndex: isActive ? 20 : 5,
              }}
              onMouseEnter={(e) => {
                setTooltip({
                  name: node.provider.name,
                  keys: node.provider.key_count ?? 0,
                  status: node.status,
                  x: e.clientX,
                  y: e.clientY,
                });
              }}
              onMouseMove={(e) => {
                setTooltip((t) => (t ? { ...t, x: e.clientX, y: e.clientY } : null));
              }}
              onMouseLeave={() => setTooltip(null)}
            >
              {isActive && (
                <>
                  <div
                    className="absolute rounded-full"
                    style={{
                      inset: -6,
                      border: `2px solid ${NEON_CYAN}`,
                      opacity: 0.4,
                      animation: "topology-ping 1.5s cubic-bezier(0, 0, 0.2, 1) infinite",
                    }}
                  />
                  <div
                    className="absolute rounded-full"
                    style={{
                      inset: -12,
                      border: `1px solid ${NEON_CYAN}`,
                      opacity: 0.15,
                      animation: "topology-ping 2s cubic-bezier(0, 0, 0.2, 1) infinite 0.3s",
                    }}
                  />
                </>
              )}

              <div
                className="w-full h-full rounded-full flex items-center justify-center transition-all duration-500 overflow-hidden"
                style={{
                  background: isActive ? `${NEON_CYAN}15` : "#0d0e18",
                  border: `2px solid ${isActive ? NEON_CYAN : color + "50"}`,
                  boxShadow: isActive
                    ? `0 0 24px ${NEON_CYAN}50, inset 0 0 12px ${NEON_CYAN}15`
                    : `0 0 6px ${color}10`,
                }}
              >
                {logo ? (
                  <Image
                    src={logo}
                    alt=""
                    width={nodeSize * 0.5}
                    height={nodeSize * 0.5}
                    className="rounded-sm object-contain"
                    unoptimized
                  />
                ) : (
                  <span
                    className="font-bold font-heading"
                    style={{
                      color: isActive ? NEON_CYAN : color,
                      fontSize: nodeSize * 0.35,
                    }}
                  >
                    {node.provider.name.charAt(0).toUpperCase()}
                  </span>
                )}
              </div>
            </div>
          );
        })}

        {tooltip && (
          <div
            className="fixed z-50 pointer-events-none rounded-lg shadow-xl px-3 py-2 text-xs border"
            style={{
              left: tooltip.x + 14,
              top: tooltip.y - 12,
              background: "rgba(15, 16, 28, 0.95)",
              borderColor: tooltip.status === "active" ? NEON_CYAN + "40" : "var(--border, rgba(255,255,255,0.08))",
              backdropFilter: "blur(12px)",
            }}
          >
            <div className="font-semibold">{tooltip.name}</div>
            <div className="text-muted-foreground mt-0.5">
              {tooltip.keys} keys
              {tooltip.status === "active" && (
                <span className="ml-2 text-neon-cyan font-mono">● LIVE</span>
              )}
            </div>
          </div>
        )}
      </div>

      <style jsx global>{`
        @keyframes topology-ping {
          0% { transform: scale(1); opacity: 0.4; }
          75%, 100% { transform: scale(1.6); opacity: 0; }
        }
      `}</style>
    </section>
  );
}
