"use client";

import { useQuery } from "@tanstack/react-query";
import { api, type CostSummary } from "@/lib/api";
import { Activity, TrendingUp, DollarSign } from "lucide-react";

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
      label: "Requests (24h)",
      value: today ? formatNumber(today.req_count) : null,
      isLoading: costQuery.isLoading,
      icon: Activity,
    },
    {
      label: "Success Rate",
      value: successRate !== null ? formatPercent(successRate) : null,
      isLoading: logsTotalQuery.isLoading || logsSuccessQuery.isLoading,
      icon: TrendingUp,
    },
    {
      label: "Cost (24h)",
      value: today ? formatCurrency(today.cost_usd) : null,
      isLoading: costQuery.isLoading,
      icon: DollarSign,
    },
  ];

  return (
    <div className="grid grid-cols-2 lg:grid-cols-3 gap-3.5 mb-7">
      {stats.map((stat) => {
        const Icon = stat.icon;
        return (
          <div
            key={stat.label}
            className="rounded-xl border border-border bg-card p-4 text-center"
          >
            <div className="flex items-center justify-center mb-2">
              <Icon className="w-4 h-4 text-muted-foreground" />
            </div>

            <div className="text-[26px] font-bold leading-none font-mono text-foreground">
              {stat.isLoading ? (
                <span className="text-muted-foreground">Loading</span>
              ) : (
                stat.value ?? "--"
              )}
            </div>

            <div className="text-xs text-muted-foreground mt-1.5">
              {stat.label}
            </div>
          </div>
        );
      })}
    </div>
  );
}
