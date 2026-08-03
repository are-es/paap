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
      <div className="relative group rounded-xl border border-primary/15 bg-primary/[0.04] p-4 transition-all duration-300 hover:border-neon-cyan/20">
        <div className="absolute inset-x-4 top-0 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-500" style={{ background: "linear-gradient(90deg, transparent, var(--neon-cyan), transparent)" }} />
        <div className="flex items-center gap-2 mb-3">
          <Terminal className="w-3.5 h-3.5 text-neon-cyan/60" />
          <span className="text-[10px] font-semibold uppercase tracking-widest text-neon-cyan/50">Base URL</span>
        </div>
        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-neon-cyan bg-background/50 border border-neon-cyan/10 rounded-lg px-3 py-2.5 truncate">{effectiveBaseUrl}</code>
          <button onClick={() => copy(effectiveBaseUrl, "url")} className="p-2 rounded-lg border border-border hover:bg-muted text-muted-foreground transition-all">
            {copied === "url" ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      <div className="relative group rounded-xl border border-primary/15 bg-primary/[0.04] p-4 transition-all duration-300 hover:border-neon-purple/20">
        <div className="absolute inset-x-4 top-0 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-500" style={{ background: "linear-gradient(90deg, transparent, var(--neon-purple), transparent)" }} />
        <div className="flex items-center gap-2 mb-3">
          <span className="text-[10px] font-semibold uppercase tracking-widest text-neon-purple/50">API Key</span>
        </div>
        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-neon-purple bg-background/50 border border-neon-purple/10 rounded-lg px-3 py-2.5 truncate">{displayKey}</code>
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
