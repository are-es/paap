"use client";

import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Provider, type LogEntry } from "@/lib/api";
import { useLogStream } from "@/lib/use-log-stream";
import { Button } from "@/components/ui/button";
import {
  Download,
  Trash2,
  ChevronDown,
  RefreshCw,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { ConfirmModal } from "@/components/ui/confirm-modal";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { useLanguage } from "@/lib/language-context";

const STATUS_OPTIONS = [
  { value: "", label: "All Status" },
  { value: "2xx", label: "2xx Success" },
  { value: "4xx", label: "4xx Error" },
  { value: "5xx", label: "5xx Error" },
  { value: "200", label: "200" },
  { value: "400", label: "400" },
  { value: "401", label: "401" },
  { value: "402", label: "402 Billing" },
  { value: "403", label: "403" },
  { value: "429", label: "429 Rate Limit" },
  { value: "500", label: "500" },
] as const;

export default function LogsPage() {
  const queryClient = useQueryClient();
  const [provider, setProvider] = useState("");
  const [status, setStatus] = useState("");
  const [period, setPeriod] = useState<"today" | "7d" | "30d" | "all">("today");

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => api.getProviders(),
  });

  const costQuery = useQuery({
    queryKey: ["cost"],
    queryFn: () => api.getCost(),
  });

  /**
   * Does a pushed row belong in the currently filtered view? The server filters
   * the initial fetch, but pushed rows are unfiltered, so the same predicate has
   * to run here or a filtered table would fill with unrelated rows.
   */
  const rowMatchesFilters = useCallback(
    (row: LogEntry): boolean => {
      if (provider && row.provider_name !== provider) return false;
      if (!status) return true;
      const code = row.status_code ?? 0;
      switch (status) {
        case "2xx":
          return code >= 200 && code < 300;
        case "4xx":
          return code >= 400 && code < 500;
        case "5xx":
          return code >= 500;
        default:
          return String(code) === status;
      }
    },
    [provider, status]
  );

  const handleStreamedRow = useCallback(
    (row: LogEntry) => {
      if (!rowMatchesFilters(row)) return;

      queryClient.setQueryData<LogEntry[]>(["logs", provider, status], (existing) => {
        const rows = existing ?? [];
        // Guard against a duplicate if a refetch raced the push.
        if (rows.some((r) => r.id === row.id)) return rows;
        // Newest first, capped at the same 500 the server returns.
        return [row, ...rows].slice(0, 500);
      });

      // The Cost/Tokens/Requests cards come from an aggregate endpoint that
      // cannot be derived from a single row, so refresh them on new traffic
      // instead of polling blindly.
      queryClient.invalidateQueries({ queryKey: ["cost"] });
    },
    [queryClient, provider, status, rowMatchesFilters]
  );

  const streamState = useLogStream<LogEntry>({
    onRow: handleStreamedRow,
    // Rows emitted while disconnected are not replayed, so resync on reconnect.
    onReconnect: () => {
      queryClient.invalidateQueries({ queryKey: ["logs"] });
      queryClient.invalidateQueries({ queryKey: ["cost"] });
    },
  });

  const logsQuery = useQuery({
    queryKey: ["logs", provider, status],
    queryFn: () =>
      api.getLogs({
        provider: provider || undefined,
        status: status || undefined,
        limit: 500,
        offset: 0,
      }),
    // Rows normally arrive by push, so no interval. If the stream dies the table
    // would silently freeze, so fall back to the old polling behaviour.
    refetchInterval: streamState === "offline" ? 5000 : false,
  });

  const cost = costQuery.data;
  const logs = logsQuery.data ?? [];
  const providers = providersQuery.data ?? [];

  const avgLatency = logs.length > 0
    ? Math.round(logs.reduce((sum, l) => sum + (l.latency_ms || 0), 0) / logs.length)
    : 0;

  // Requests whose price could not be resolved, or that predate the billing fix.
  // Their cost_usd is 0 rather than a guess, so they must be reported separately
  // instead of silently lowering the spend figure.
  const unpricedLogs = logs.filter(
    (l) => l.pricing_source === "missing" || l.pricing_source === "legacy"
  );
  const unpricedTokens = unpricedLogs.reduce(
    (sum, l) => sum + (l.tokens_in ?? 0) + (l.tokens_out ?? 0),
    0
  );

  const [confirmClear, setConfirmClear] = useState(false);
  const [showDocs, setShowDocs] = useState(false);
  const { t } = useLanguage();

  const handleClear = async () => {
    setConfirmClear(false);
    await api.clearLogs();
    queryClient.invalidateQueries({ queryKey: ["logs"] });
  };

  const formatCost = (value?: number) => `$${(value ?? 0).toFixed(2)}`;

  const formatTokens = (n: number): string => {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
    if (n >= 1_000) return (n / 1_000).toFixed(1).replace(/\.0$/, "") + "K";
    return n.toString();
  };

  const statusColor = (code: number) => {
    if (code >= 200 && code < 300) return "text-green-600";
    if (code >= 400 && code < 500) return "text-amber-600";
    if (code >= 500) return "text-red-500";
    return "text-muted-foreground";
  };

  const shortProxy = (proxy?: string) => {
    if (!proxy) return "-";
    // Extract ip:port from socks5://ip:port or http://ip:port
    const match = proxy.match(/\/\/([^/]+)/);
    return match ? match[1] : proxy;
  };

  /**
   * Hover text for the token cell. tokens_in is the context-window sum; the
   * components below are priced at different rates (cache reads are cheaper,
   * cache writes are more expensive than fresh input).
   */
  const tokenBreakdown = (log: LogEntry): string => {
    const parts: string[] = [];
    if ((log.tokens_in_fresh ?? 0) > 0) parts.push(`fresh in: ${log.tokens_in_fresh}`);
    if ((log.tokens_cache_read ?? 0) > 0) parts.push(`cache read: ${log.tokens_cache_read}`);
    if ((log.tokens_cache_write ?? 0) > 0) parts.push(`cache write: ${log.tokens_cache_write}`);
    parts.push(`out: ${log.tokens_out ?? 0}`);
    if ((log.tokens_reasoning ?? 0) > 0) parts.push(`reasoning: ${log.tokens_reasoning}`);
    if (log.tokens_estimated) parts.push("(estimated — provider reported no usage)");
    return parts.join(" · ");
  };

  return (
    <div className="p-6 md:p-8 min-h-full flex flex-col gap-5">
      {/* PAGE HEADER */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div className="flex items-center gap-3">
          <h1 className="font-heading text-xl font-bold">Logs</h1>
          <DocsButton onClick={() => setShowDocs(true)} />
          {/*
            Stream state, not fetch state. "live" means rows are pushed as
            requests finish; "polling" means the stream dropped and the table
            fell back to a 5s refetch.
          */}
          {streamState === "live" ? (
            <span
              className="inline-flex items-center gap-1.5 text-[10px] font-mono text-emerald-600"
              title="Connected to the live log stream — rows appear as requests finish"
            >
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
              live
            </span>
          ) : streamState === "connecting" ? (
            <span className="inline-flex items-center gap-1.5 text-[10px] font-mono text-muted-foreground">
              <RefreshCw className="w-3 h-3 animate-spin" />
              connecting
            </span>
          ) : (
            <span
              className="inline-flex items-center gap-1.5 text-[10px] font-mono text-amber-600"
              title="Live stream unavailable — falling back to polling every 5s"
            >
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
              polling
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <select
            className="text-[11px] font-mono bg-muted/50 border border-border rounded px-2 py-1.5 text-foreground appearance-none cursor-pointer"
            value={period}
            onChange={(e) => setPeriod(e.target.value as typeof period)}
          >
            <option value="today">Today</option>
            <option value="7d">7 Days</option>
            <option value="30d">30 Days</option>
            <option value="all">All Time</option>
          </select>
          <a
            href={api.exportLogs("csv")}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs rounded border border-border text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
          >
            <Download className="w-3 h-3" />
            Export CSV
          </a>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmClear(true)}
            className="gap-1 text-xs"
          >
            <Trash2 className="w-3 h-3" />
            Clear Logs
          </Button>
        </div>
      </div>

      {/* STATS CARDS */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div className="bg-card border border-border rounded-lg p-3">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Cost</div>
          <div className="font-mono text-lg font-bold">{formatCost(cost?.summary[period]?.cost_usd)}</div>
          {unpricedLogs.length > 0 && (
            <div
              className="text-[10px] font-mono text-amber-600"
              title="These requests have no matching price row (or predate the billing fix). Their cost is recorded as 0, not estimated."
            >
              {unpricedLogs.length} unpriced ({formatTokens(unpricedTokens)} tok)
            </div>
          )}
        </div>
        <div className="bg-card border border-border rounded-lg p-3">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Tokens</div>
          <div className="font-mono text-lg font-bold">
            {formatTokens((cost?.summary[period]?.tokens_in ?? 0) + (cost?.summary[period]?.tokens_out ?? 0))}
          </div>
          <div className="text-[10px] text-muted-foreground font-mono flex items-center gap-2">
            <span className="flex items-center gap-0.5">
              <svg className="w-3 h-3 text-green-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
              {formatTokens(cost?.summary[period]?.tokens_in ?? 0)}
            </span>
            <span className="flex items-center gap-0.5">
              <svg className="w-3 h-3 text-red-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12l7 7 7-7"/></svg>
              {formatTokens(cost?.summary[period]?.tokens_out ?? 0)}
            </span>
          </div>
        </div>
        <div className="bg-card border border-border rounded-lg p-3">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Latency</div>
          <div className="font-mono text-lg font-bold">{avgLatency}<span className="text-xs text-muted-foreground">ms</span></div>
        </div>
        <div className="bg-card border border-border rounded-lg p-3">
          <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-1">Requests</div>
          <div className="font-mono text-lg font-bold">{(cost?.summary[period]?.req_count ?? 0).toLocaleString()}</div>
        </div>
      </div>

      {/* FILTERS */}
      <div className="flex flex-col sm:flex-row gap-3">
        <div className="relative flex-1 min-w-[180px]">
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="w-full appearance-none pl-3 pr-9 py-2 rounded-lg border border-input bg-background text-sm focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none"
          >
            <option value="">All Providers</option>
            {providers.map((p: Provider) => (
              <option key={p.id} value={p.name}>{p.name}</option>
            ))}
          </select>
          <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
        </div>
        <div className="relative w-[140px]">
          <select
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className="w-full appearance-none pl-3 pr-9 py-2 rounded-lg border border-input bg-background text-sm focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:outline-none"
          >
            {STATUS_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
          <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
        </div>
      </div>

      {/* LOGS TABLE */}
      <div className="flex-1 min-h-0 rounded-lg border border-border bg-card p-3 overflow-hidden">
        <div className="overflow-auto rounded-md" style={{ height: "calc(100vh - 390px)" }}>
          <table className="w-full text-xs" style={{ minWidth: 700 }}>
            <thead className="sticky top-0 z-10 bg-card/95 backdrop-blur-sm">
              <tr className="border-b border-border">
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground whitespace-nowrap">Time</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Proxy</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Key</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground">Provider</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground">Model</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground hidden xl:table-cell">Tool</th>
                <th className="px-3.5 py-2.5 text-left font-medium text-muted-foreground hidden xl:table-cell">Group</th>
                <th className="px-3.5 py-2.5 text-center font-medium text-muted-foreground">Status</th>
                <th className="px-3.5 py-2.5 text-right font-medium text-muted-foreground">
                  <span className="inline-flex items-center gap-1">
                    <svg className="w-3 h-3 text-green-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
                    /
                    <svg className="w-3 h-3 text-red-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 5v14M5 12l7 7 7-7"/></svg>
                  </span>
                </th>
                <th className="px-3.5 py-2.5 text-right font-medium text-muted-foreground">Latency</th>
                <th className="px-3.5 py-2.5 text-right font-medium text-muted-foreground">Cost</th>
              </tr>
            </thead>
            <tbody>
              {logs.length === 0 && (
                <tr>
                  <td colSpan={11} className="text-center py-12 text-muted-foreground text-sm">
                    {logsQuery.isLoading ? "Loading..." : "No logs found"}
                  </td>
                </tr>
              )}
              {logs.map((log) => {
                const totalTokens = (log.tokens_in ?? 0) + (log.tokens_out ?? 0);

                return (
                  <tr
                    key={log.id}
                    className="border-b border-border/30 hover:bg-muted/30 transition-colors"
                  >
                    <td className="px-3.5 py-2.5 font-mono text-muted-foreground whitespace-nowrap">
                      {new Date(log.timestamp).toLocaleString()}
                    </td>
                    <td className="px-3.5 py-2.5 font-mono text-muted-foreground hidden lg:table-cell">
                      {shortProxy(log.proxy_used)}
                    </td>
          <td className="px-3.5 py-2.5 font-mono text-muted-foreground text-xs hidden lg:table-cell">
            {log.key_name || "-"}
          </td>
                    <td className="px-3.5 py-2.5">
                      {log.provider_name}
                    </td>
                    <td className="px-3.5 py-2.5 font-mono text-muted-foreground">
                      {log.model_id}
                    </td>
                    <td className="px-3.5 py-2.5 hidden xl:table-cell">
                      {log.tool_used ? (
                        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-primary/10 text-primary text-[10px] font-medium" title={`Original: ${log.original_model}`}>
                          {log.tool_used}
                        </span>
                      ) : "-"}
                    </td>
                    <td className="px-3.5 py-2.5 text-muted-foreground hidden xl:table-cell">
                      {log.group_name || "-"}
                    </td>
                    <td className={cn("px-3.5 py-2.5 text-center font-mono font-medium", statusColor(log.status_code))}>
                      {log.status_code}
                    </td>
                    <td className="px-3.5 py-2.5 text-right font-mono">
                      {totalTokens > 0 ? (
                        <span
                          className="inline-flex items-center gap-1.5"
                          title={tokenBreakdown(log)}
                        >
                          <span className="text-green-600">{formatTokens(log.tokens_in ?? 0)}</span>
                          <span className="text-muted-foreground">/</span>
                          <span className="text-red-400">{formatTokens(log.tokens_out ?? 0)}</span>
                          {(log.tokens_cache_read ?? 0) > 0 && (
                            <span className="text-[9px] text-blue-500 font-semibold" title="Portion served from provider cache (billed at a reduced rate)">
                              cached
                            </span>
                          )}
                          {log.tokens_estimated && (
                            <span className="text-[9px] text-amber-600 font-semibold" title="Provider reported no usage; these counts are estimates">
                              est
                            </span>
                          )}
                        </span>
                      ) : "-"}
                    </td>
                    <td className="px-3.5 py-2.5 text-right font-mono text-muted-foreground whitespace-nowrap">
                      {log.latency_ms ? `${log.latency_ms}ms` : "-"}
                    </td>
                    <td className="px-3.5 py-2.5 text-right font-mono text-muted-foreground whitespace-nowrap">
                      {log.pricing_source === "missing" || log.pricing_source === "legacy" ? (
                        <span
                          className="text-amber-600"
                          title={
                            log.pricing_source === "missing"
                              ? "No price row matched this provider/model. Cost is recorded as 0 rather than guessed."
                              : "Priced before the billing fix. Run reconcile to recompute."
                          }
                        >
                          {log.pricing_source === "missing" ? "unpriced" : "legacy"}
                        </span>
                      ) : log.pricing_source === "subscription" || log.pricing_source === "free" ? (
                        <span
                          className="text-muted-foreground"
                          title={`Provider billing mode: ${log.pricing_source}. Not charged per token.`}
                        >
                          $0.0000
                        </span>
                      ) : log.cost_usd !== undefined ? (
                        `$${log.cost_usd.toFixed(4)}`
                      ) : (
                        "-"
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div className="flex items-center justify-end">
        <span className="text-[11px] text-muted-foreground font-mono">{logs.length} entries</span>
      </div>

      <ConfirmModal
        open={confirmClear}
        title="Clear Logs"
        message="Clear all logs? Cost data will NOT be affected."
        confirmLabel="Clear All"
        variant="danger"
        onConfirm={handleClear}
        onCancel={() => setConfirmClear(false)}
      />

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title={t("logs_docs_title")}
        sections={[
          { title: t("logs_docs_overview_title"), content: t("logs_docs_overview_content") },
          { title: t("logs_docs_usage_title"), content: t("logs_docs_usage_content") },
          { title: t("logs_docs_troubleshoot_title"), content: t("logs_docs_troubleshoot_content") },
        ]}
      />
    </div>
  );
}
