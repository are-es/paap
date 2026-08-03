"use client";

import { useMemo, useState, useRef, useEffect } from "react";
import Image from "next/image";
import type { Provider } from "@/lib/api";
import { getProviderLogo } from "@/lib/provider-logos";

interface ProviderTopologyProps {
  providers: Provider[];
}

const NEON_CYAN = "#22d3ee";
const NEON_AMBER = "#fbbf24";

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

export function ProviderTopology({ providers }: ProviderTopologyProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [activeProviders, setActiveProviders] = useState<Set<string>>(new Set());
  const [tooltip, setTooltip] = useState<{ name: string; info: string; x: number; y: number } | null>(null);
  const [dims, setDims] = useState({ w: 700, h: 420 });

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
    const interval = setInterval(poll, 5000);
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

  const onlineProviders = providers.filter((p) => p.status !== "offline");
  const cx = dims.w / 2;
  const paapY = 50;
  const provY = 300;
  const hasActive = activeProviders.size > 0;

  const provPositions = useMemo(() => {
    const count = onlineProviders.length;
    if (count === 0) return [];
    const spacing = Math.min(120, (dims.w - 80) / count);
    const startX = cx - ((count - 1) * spacing) / 2;
    return onlineProviders.map((_, i) => startX + i * spacing);
  }, [onlineProviders.length, cx, dims.w]);

  const paths = useMemo(() => {
    const result: { d: string; key: string; provName: string }[] = [];
    onlineProviders.forEach((prov, i) => {
      const px = provPositions[i];
      if (!px) return;
      // Single line: PAAP → Provider (request direction)
      result.push({
        d: `M ${cx} ${paapY + 48} C ${cx} ${paapY + 130} ${px} ${provY - 100} ${px} ${provY - 4}`,
        key: `line-${prov.id}`,
        provName: prov.name,
      });
    });
    return result;
  }, [onlineProviders, provPositions, cx]);

  return (
    <section className="mb-7" aria-label="Live Activity">
      <div className="flex items-center justify-between mb-4">
        <h2 className="font-heading text-lg font-bold flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-neon-cyan animate-pulse" />
          Live Activity
        </h2>
        <div className="flex items-center gap-4 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: NEON_CYAN }} /> Request
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: "rgba(34,211,238,0.35)" }} /> Response
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full" style={{ background: NEON_AMBER }} /> Provider
          </span>
        </div>
      </div>

      <div
        ref={containerRef}
        className="rounded-2xl border border-border min-h-[380px] relative overflow-hidden"
        style={{
          background: "var(--background, #08080f)",
          backgroundImage: "radial-gradient(circle, var(--border, rgba(255,255,255,0.025)) 1px, transparent 1px)",
          backgroundSize: "18px 18px",
        }}
      >
        {/* Layer tags */}
        <div className="absolute left-4 text-[9px] font-bold tracking-[1.5px] uppercase opacity-40" style={{ top: 28, color: NEON_CYAN }}>Gateway</div>
        <div className="absolute left-4 text-[9px] font-bold tracking-[1.5px] uppercase opacity-40" style={{ top: provY - 28, color: NEON_AMBER }}>Providers</div>

        {/* SVG edges */}
        <svg className="absolute inset-0 w-full h-full pointer-events-none z-[4]">
          {paths.map((p) => {
            const isActive = activeProviders.has(p.provName);

            const color = isActive ? NEON_CYAN : "var(--muted-foreground, #64748b)";
            const opacity = isActive ? 0.55 : 0.25;
            const width = isActive ? 1.8 : 1;
            const dashArray = "6,5";

            return (
              <g key={p.key}>
                <path
                  d={p.d}
                  fill="none"
                  stroke={color}
                  strokeWidth={width}
                  strokeDasharray={dashArray}
                  strokeLinecap="round"
                  opacity={opacity}
                  style={{
                    animation: isActive ? "flow-down 1.8s linear infinite" : "none",
                  }}
                />
                {isActive && (
                  <>
                    <circle r={3} fill={NEON_CYAN} opacity={0.9}>
                      <animateMotion dur="1.6s" repeatCount="indefinite" path={p.d} />
                    </circle>
                    <circle r={1.5} fill="#fff" opacity={0.6}>
                      <animateMotion dur="1.6s" repeatCount="indefinite" path={p.d} />
                    </circle>
                  </>
                )}
              </g>
            );
          })}
        </svg>

        {/* PAAP Node */}
        <div
          className="absolute z-10 cursor-pointer flex flex-col items-center"
          style={{ top: paapY - 8, left: cx - 45, width: 90 }}
          onMouseEnter={(e) => setTooltip({ name: "PAAP", info: "Gateway", x: e.clientX, y: e.clientY })}
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

        {/* Provider Nodes */}
        {onlineProviders.map((prov, i) => {
          const px = provPositions[i];
          if (!px) return null;
          const isActive = activeProviders.has(prov.name);
          const color = providerColor(prov);
          const logo = getProviderLogo(prov);
          return (
            <div
              key={prov.id}
              className="absolute z-10 cursor-pointer flex flex-col items-center"
              style={{ top: provY - 4, left: px - 30, width: 60 }}
              onMouseEnter={(e) => setTooltip({ name: prov.name, info: `${prov.key_count ?? 0} keys · ${prov.status}`, x: e.clientX, y: e.clientY })}
              onMouseLeave={() => setTooltip(null)}
            >
              <div
                className="w-[50px] h-[50px] rounded-[13px] flex items-center justify-center overflow-hidden transition-all"
                style={{
                  background: "var(--background, #0f0f1a)",
                  border: `2px solid ${isActive ? color + "80" : color + "30"}`,
                  boxShadow: isActive
                    ? `0 0 20px ${color}50, 0 2px 8px rgba(0,0,0,0.4)`
                    : `0 0 16px ${color}15, 0 2px 8px rgba(0,0,0,0.4)`,
                }}
              >
                {logo ? (
                  <Image src={logo} alt="" width={26} height={26} className="rounded-sm object-contain" unoptimized />
                ) : (
                  <span className="text-base font-bold" style={{ color }}>{prov.name.charAt(0)}</span>
                )}
              </div>
              <span className="text-[10px] font-semibold mt-1 truncate max-w-[60px] text-center" style={{ color }}>{prov.name}</span>
            </div>
          );
        })}

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
      `}</style>
    </section>
  );
}
