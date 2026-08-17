"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  ChevronRight,
  Search,
  Loader2,
  Check,
  AlertCircle,
  Globe,
  Zap,
  Shield,
  Server,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface SearchProvider {
  id: string;
  name: string;
  icon: any;
  color: string;
  description: string;
  quality: string;
  needsKey: boolean;
  keyPlaceholder: string;
  settingKey: string;
  free: boolean;
}

const providers: SearchProvider[] = [
  {
    id: "firecrawl",
    name: "Firecrawl",
    icon: Zap,
    color: "text-orange-500",
    description: "Best quality web search with clean extraction. Premium API.",
    quality: "★★★★★",
    needsKey: true,
    keyPlaceholder: "fc-xxxxxxxxxxxxxxxxxxxxxxxx",
    settingKey: "search_firecrawl_key",
    free: false,
  },
  {
    id: "brave",
    name: "Brave Search",
    icon: Shield,
    color: "text-red-500",
    description: "Independent search engine with good results. Free tier: 2K/month.",
    quality: "★★★★☆",
    needsKey: true,
    keyPlaceholder: "BSAxxxxxxxxxxxxxxxxxxxxxx",
    settingKey: "search_brave_key",
    free: false,
  },
  {
    id: "searxng",
    name: "SearXNG",
    icon: Server,
    color: "text-purple-500",
    description: "Self-hosted meta search engine. Aggregates multiple sources.",
    quality: "★★★★☆",
    needsKey: true,
    keyPlaceholder: "http://localhost:8888",
    settingKey: "search_searxng_url",
    free: true,
  },
  {
    id: "duckduckgo",
    name: "DuckDuckGo",
    icon: Globe,
    color: "text-green-500",
    description: "Free search engine. No API key needed. Always available as fallback.",
    quality: "★★★☆☆",
    needsKey: false,
    keyPlaceholder: "",
    settingKey: "",
    free: true,
  },
];

export default function WebSearchSetupPage() {
  const queryClient = useQueryClient();

  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
  });
  const settings: any = settingsQuery.data || {};

  const [mode, setMode] = useState("auto");
  const [selectedProvider, setSelectedProvider] = useState("duckduckgo");
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [initialized, setInitialized] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (settings && !initialized) {
      setMode(settings.search_provider_mode || "auto");
      setSelectedProvider(settings.search_provider || "duckduckgo");
      setKeys({
        search_firecrawl_key: settings.search_firecrawl_key || "",
        search_brave_key: settings.search_brave_key || "",
        search_searxng_url: settings.search_searxng_url || "",
        search_google_key: settings.search_google_key || "",
        search_google_cx: settings.search_google_cx || "",
      });
      setInitialized(true);
    }
  }, [settings, initialized]);

  const updateSettingMutation = useMutation({
    mutationFn: (data: any) => api.updateSettings(data),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["settings"] }),
  });

  const handleSave = () => {
    updateSettingMutation.mutate({
      search_provider_mode: mode,
      search_provider: selectedProvider,
      ...keys,
    });
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const resp = await fetch("/mcp/message", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 99,
          method: "tools/call",
          params: {
            name: "web_search",
            arguments: { query: "what is the capital of france", limit: 3 },
          },
        }),
      });
      const data = await resp.json();
      const text =
        data?.result?.content?.[0]?.text || JSON.stringify(data, null, 2);
      setTestResult(text.substring(0, 500));
    } catch (e: any) {
      setTestResult("Error: " + e.message);
    }
    setTesting(false);
  };

  if (settingsQuery.isLoading) {
    return (
      <div className="p-6 md:p-8 min-h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/tools" className="hover:text-foreground transition-colors">
          Tools
        </Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <Link
          href="/tools/mcp"
          className="hover:text-foreground transition-colors"
        >
          MCP
        </Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">Web Search</span>
      </nav>

      {/* Header */}
      <div className="flex items-center justify-between gap-4 mb-8">
        <div>
          <h1 className="font-heading text-xl font-bold flex items-center gap-2">
            <Search className="w-5 h-5 text-blue-500" />
            Web Search Configuration
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            Configure search providers for the MCP web_search tool. Auto mode
            tries providers in order.
          </p>
        </div>
      </div>

      {/* Mode Selector */}
      <div className="mb-6">
        <label className="text-sm font-medium mb-2 block">Mode</label>
        <div className="flex gap-2">
          <button
            onClick={() => setMode("auto")}
            className={cn(
              "px-4 py-2 text-sm rounded-lg border transition-colors",
              mode === "auto"
                ? "border-primary bg-primary/10 text-primary"
                : "border-input hover:bg-accent"
            )}
          >
            Auto (Fallback)
          </button>
          <button
            onClick={() => setMode("manual")}
            className={cn(
              "px-4 py-2 text-sm rounded-lg border transition-colors",
              mode === "manual"
                ? "border-primary bg-primary/10 text-primary"
                : "border-input hover:bg-accent"
            )}
          >
            Manual (Pick One)
          </button>
        </div>
        <p className="text-xs text-muted-foreground mt-1">
          {mode === "auto"
            ? "Tries Firecrawl → Brave → SearXNG → DuckDuckGo. Skips providers without API keys."
            : "Uses only the selected provider. Fails if key is missing."}
        </p>
      </div>

      {/* Provider Cards */}
      <div className="space-y-3 mb-6">
        {providers.map((p) => {
          const Icon = p.icon;
          const isActive =
            mode === "auto" || selectedProvider === p.id;
          const hasKey = !p.needsKey || (keys[p.settingKey] || "").length > 0;

          return (
            <div
              key={p.id}
              className={cn(
                "border rounded-xl bg-card p-4 transition-colors",
                isActive && hasKey
                  ? "border-primary/50"
                  : "border-border"
              )}
            >
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-3">
                  {mode === "manual" && (
                    <input
                      type="radio"
                      name="provider"
                      checked={selectedProvider === p.id}
                      onChange={() => setSelectedProvider(p.id)}
                      className="accent-primary"
                    />
                  )}
                  <div
                    className={cn(
                      "w-8 h-8 rounded-lg flex items-center justify-center",
                      p.color.replace("text-", "bg-") + "/10"
                    )}
                  >
                    <Icon className={cn("w-4 h-4", p.color)} />
                  </div>
                  <div>
                    <span className="text-sm font-medium">{p.name}</span>
                    <span className="text-xs text-muted-foreground ml-2">
                      {p.quality}
                    </span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {p.free && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-green-500">
                      FREE
                    </span>
                  )}
                  {hasKey ? (
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-500 flex items-center gap-1">
                      <Check className="w-3 h-3" /> Ready
                    </span>
                  ) : p.needsKey ? (
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                      No Key
                    </span>
                  ) : null}
                </div>
              </div>

              <p className="text-xs text-muted-foreground mb-3">
                {p.description}
              </p>

              {p.needsKey && (
                <div className="flex gap-2">
                  <input
                    type={p.id === "searxng" ? "url" : "password"}
                    value={keys[p.settingKey] || ""}
                    onChange={(e) =>
                      setKeys({ ...keys, [p.settingKey]: e.target.value })
                    }
                    placeholder={p.keyPlaceholder}
                    className="flex-1 px-3 py-1.5 text-xs rounded-lg border border-input bg-background focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Actions */}
      <div className="flex gap-3">
        <button
          onClick={handleSave}
          disabled={updateSettingMutation.isPending}
          className="px-4 py-2 text-sm rounded-lg bg-primary text-primary-foreground hover:opacity-90 transition-colors disabled:opacity-50"
        >
          {updateSettingMutation.isPending ? (
            <Loader2 className="w-4 h-4 animate-spin inline mr-1" />
          ) : null}
          Save
        </button>
        <button
          onClick={handleTest}
          disabled={testing}
          className="px-4 py-2 text-sm rounded-lg border border-input hover:bg-accent transition-colors disabled:opacity-50"
        >
          {testing ? (
            <Loader2 className="w-4 h-4 animate-spin inline mr-1" />
          ) : (
            <Search className="w-4 h-4 inline mr-1" />
          )}
          Test Search
        </button>
      </div>

      {/* Test Result */}
      {testResult && (
        <div className="mt-4 border border-border rounded-xl bg-card p-4">
          <h3 className="text-xs font-medium mb-2 flex items-center gap-1">
            <AlertCircle className="w-3 h-3" /> Test Result
          </h3>
          <pre className="text-xs text-muted-foreground whitespace-pre-wrap font-mono">
            {testResult}
          </pre>
        </div>
      )}
    </div>
  );
}
