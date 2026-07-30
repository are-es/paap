"use client";

import { useQuery } from "@tanstack/react-query";
import { api, type CostSummary } from "@/lib/api";
import { Activity, TrendingUp, DollarSign, Zap } from "lucide-react";

interface LogsMeta {
  total: number;
}

async function fetchLogsMeta(status?: string): Promise<LogsMeta> {
  const qs = new URLSearchParams();
  qs.set("limit", "1");
  qs.set("offset", "0");
  if (status) qs.set("status", status);
  const res = await fetch(`/api/logs?${qs}`);
  if (!res.ok) throw new Error("Failed to fetch logs");
  const data = (await res.json()) as { total: number };
  return { total: data.total ?? 0 };
}

function formatNumber(n: number) {
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(n);
}

function formatCurrency(n: number) {
  return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }).format(n);
}

function formatPercent(n: number) {
  return `${n.toFixed(1)}%`;
}

const NEON = {
  cyan: { color: "#22d3ee", glow: "rgba(34,211,238,0.25)", border: "rgba(34,211,238,0.3)" },
  green: { color: "#34d399", glow: "rgba(52,211,153,0.25)", border: "rgba(52,211,153,0.3)" },
  amber: { color: "#fbbf24", glow: "rgba(251,191,36,0.25)", border: "rgba(251,191,36,0.3)" },
  purple: { color: "#a78bfa", glow: "rgba(167,139,250,0.25)", border: "rgba(167,139,250,0.3)" },
};

export function StatsBar() {
  const costQuery = useQuery<CostSummary>({
    queryKey: ["logs-cost"],
    queryFn: () => api.getCost(),
    refetchInterval: 30_000,
  });

  const logsTotalQuery = useQuery<LogsMeta>({
    queryKey: ["logs-total"],
    queryFn: () => fetchLogsMeta(),
    refetchInterval: 30_000,
  });

  const logsSuccessQuery = useQuery<LogsMeta>({
    queryKey: ["logs-success"],
    queryFn: () => fetchLogsMeta("200"),
    refetchInterval: 30_000,
  });

  const summary = costQuery.data?.summary;
  const today = summary?.today;
  const total = logsTotalQuery.data?.total ?? 0;
  const success = logsSuccessQuery.data?.total ?? 0;
  const successRate = total > 0 ? (success / total) * 100 : null;

  const stats = [
    {
      label: "reqs (24h)",
      value: today ? formatNumber(today.req_count) : null,
      isLoading: costQuery.isLoading,
      icon: Activity,
      neon: NEON.cyan,
    },
    {
      label: "Success Rate",
      value: successRate !== null ? formatPercent(successRate) : null,
      isLoading: logsTotalQuery.isLoading || logsSuccessQuery.isLoading,
      icon: TrendingUp,
      neon: NEON.green,
    },
    {
      label: "Cost (24h)",
      value: today ? formatCurrency(today.cost_usd) : null,
      isLoading: costQuery.isLoading,
      icon: DollarSign,
      neon: NEON.amber,
    },
    {
      icon: Zap,
      neon: NEON.purple,
    },
  ];

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3.5 mb-7">
      {stats.map((stat) => {
        const Icon = stat.icon;
        return (
          <div
            key={stat.label}
            className="group relative rounded-xl border border-border bg-card p-4 text-center transition-all duration-300 hover:border-foreground/10"
            style={{
              "--card-neon": stat.neon.color,
              "--card-glow": stat.neon.glow,
              "--card-border": stat.neon.border,
            } as React.CSSProperties}
          >
            <div
              className="absolute inset-x-4 top-0 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-500"
              style={{
                background: `linear-gradient(90deg, transparent, ${stat.neon.color}, transparent)`,
                boxShadow: `0 0 12px ${stat.neon.glow}`,
              }}
            />

            <div className="flex items-center justify-center mb-2">
              <Icon
                className="w-4 h-4 transition-colors duration-300"
                style={{ color: stat.neon.color + "80" }}
              />
            </div>

            <div
              className="text-[26px] font-bold leading-none font-mono transition-all duration-300"
              style={{ color: stat.value !== null ? stat.neon.color : "#3f3f46" }}
            >
              {stat.isLoading ? (
                <div className="flex justify-center">
                  <div
                    className="w-16 h-7 rounded-md animate-pulse"
                    style={{
                      background: `linear-gradient(90deg, transparent, ${stat.neon.glow}, transparent)`,
                      backgroundSize: "200% 100%",
                      animation: "neon-shimmer 1.5s ease-in-out infinite",
                    }}
                  />
                </div>
              ) : (
                <span style={{ textShadow: stat.value ? `0 0 20px ${stat.neon.glow}` : "none" }}>
                  {stat.value ?? "—"}
                </span>
              )}
            </div>

            <div className="text-[11px] text-muted-foreground font-medium mt-1.5 uppercase tracking-wider">
              {stat.label}
            </div>
          </div>
        );
      })}

      <style jsx global>{`
        @keyframes neon-shimmer {
          0% { background-position: 200% 0; }
          100% { background-position: -200% 0; }
        }
      `}</style>
    </div>
  );
}
