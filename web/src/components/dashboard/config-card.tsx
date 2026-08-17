"use client";

import { useState, useCallback } from "react";
import { Copy, Eye, EyeOff, Terminal, Check } from "lucide-react";

function maskKey(key: string) {
  if (key.length <= 8) return "•".repeat(key.length);
  return `${key.slice(0, 4)}${"•".repeat(key.length - 8)}${key.slice(-4)}`;
}

export function ConfigCard({ baseUrl, apiKey }: { baseUrl?: string; apiKey?: string }) {
  const [showKey, setShowKey] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);

  const copy = useCallback(async (text: string, id: string) => {
    await navigator.clipboard.writeText(text);
    setCopied(id);
    setTimeout(() => setCopied(null), 1500);
  }, []);

  const effectiveBaseUrl = baseUrl || (typeof window !== "undefined" ? `${window.location.origin}/v1` : "");
  const effectiveKey = apiKey || (typeof window !== "undefined" ? localStorage.getItem("paap_gateway_key") : "") || "";
  const displayKey = showKey ? effectiveKey : maskKey(effectiveKey);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
      <div className="rounded-xl border border-primary/15 bg-primary/[0.04] p-4">
        <div className="flex items-center gap-2 mb-3">
          <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
          <span className="text-xs text-muted-foreground">Base URL</span>
        </div>
        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-foreground bg-background/50 border border-border rounded-lg px-3 py-2.5 truncate">{effectiveBaseUrl}</code>
          <button onClick={() => copy(effectiveBaseUrl, "url")} className="p-2 rounded-lg border border-border hover:bg-muted text-muted-foreground transition-all">
            {copied === "url" ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      <div className="rounded-xl border border-primary/15 bg-primary/[0.04] p-4">
        <div className="flex items-center gap-2 mb-3">
          <span className="text-xs text-muted-foreground">API Key</span>
        </div>
        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-foreground bg-background/50 border border-border rounded-lg px-3 py-2.5 truncate">{displayKey}</code>
          <button onClick={() => setShowKey(!showKey)} className="p-2 rounded-lg border border-border hover:bg-muted text-muted-foreground transition-all">
            {showKey ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
          </button>
          <button onClick={() => copy(effectiveKey, "key")} className="p-2 rounded-lg border border-border hover:bg-muted text-muted-foreground transition-all">
            {copied === "key" ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>
    </div>
  );
}
