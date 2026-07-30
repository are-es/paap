"use client";

import { useState, useCallback } from "react";
import { Copy, Eye, EyeOff, Terminal, Check } from "lucide-react";
import { cn } from "@/lib/utils";

interface ConfigCardProps {
  baseUrl?: string;
  apiKey?: string;
}

function useClipboard() {
  const [copied, setCopied] = useState(false);
  const copy = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {}
  }, []);
  return { copied, copy };
}

function maskKey(key: string) {
  if (key.length <= 8) return "•".repeat(key.length);
  return `${key.slice(0, 4)}${"•".repeat(key.length - 8)}${key.slice(-4)}`;
}

export function ConfigCard({ baseUrl, apiKey }: ConfigCardProps) {
  const [showKey, setShowKey] = useState(false);
  const { copied: copiedUrl, copy: copyUrl } = useClipboard();
  const { copied: copiedKey, copy: copyKey } = useClipboard();

  const effectiveBaseUrl =
    baseUrl || (typeof window !== "undefined" ? `${window.location.origin}/v1` : "http://localhost:${window.location.port}/v1");
  const effectiveKey =
    apiKey || (typeof window !== "undefined" ? localStorage.getItem("paap_gateway_key") : "") || "";

  const displayKey = showKey ? effectiveKey : maskKey(effectiveKey);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
      <div className="relative group rounded-xl border border-border bg-card p-4 transition-all duration-300 hover:border-neon-cyan/20">
        <div
          className="absolute inset-x-4 top-0 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-500"
          style={{
            background: "linear-gradient(90deg, transparent, var(--neon-cyan), transparent)",
          }}
        />

        <div className="flex items-center gap-2 mb-3">
          <Terminal className="w-3.5 h-3.5 text-neon-cyan/60" />
          <span className="text-[10px] font-semibold uppercase tracking-widest text-neon-cyan/50">
            Base URL
          </span>
        </div>

        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-neon-cyan bg-background/50 border border-neon-cyan/10 rounded-lg px-3 py-2.5 truncate">
            {effectiveBaseUrl}
          </code>
          <button
            type="button"
            onClick={() => copyUrl(effectiveBaseUrl)}
            className={cn(
              "inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border font-mono text-[11px] transition-all duration-200",
              copiedUrl
                ? "border-neon-cyan/40 text-neon-cyan bg-neon-cyan/10"
                : "border-neon-cyan/15 text-neon-cyan/60 bg-transparent hover:border-neon-cyan/30 hover:text-neon-cyan hover:bg-neon-cyan/5"
            )}
            aria-label="Copy base URL"
          >
            {copiedUrl ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
            {copiedUrl ? "Copied" : "Copy"}
          </button>
        </div>
      </div>

      <div className="relative group rounded-xl border border-border bg-card p-4 transition-all duration-300 hover:border-neon-cyan/20">
        <div
          className="absolute inset-x-4 top-0 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-500"
          style={{
            background: "linear-gradient(90deg, transparent, var(--neon-cyan), transparent)",
          }}
        />

        <div className="flex items-center gap-2 mb-3">
          <Terminal className="w-3.5 h-3.5 text-neon-cyan/60" />
          <span className="text-[10px] font-semibold uppercase tracking-widest text-neon-cyan/50">
            Gateway API Key
          </span>
        </div>

        <div className="flex items-center gap-3">
          <code className="flex-1 font-mono text-[13px] text-neon-cyan bg-background/50 border border-neon-cyan/10 rounded-lg px-3 py-2.5 truncate">
            {effectiveKey ? displayKey : "—"}
          </code>
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => setShowKey((s) => !s)}
              className={cn(
                "inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border font-mono text-[11px] transition-all duration-200",
                "border-neon-cyan/15 text-neon-cyan/60 bg-transparent hover:border-neon-cyan/30 hover:text-neon-cyan hover:bg-neon-cyan/5"
              )}
              aria-label={showKey ? "Hide API key" : "Show API key"}
            >
              {showKey ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
              {showKey ? "Hide" : "Show"}
            </button>
            <button
              type="button"
              onClick={() => copyKey(effectiveKey)}
              disabled={!effectiveKey}
              className={cn(
                "inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border font-mono text-[11px] transition-all duration-200",
                copiedKey
                  ? "border-neon-cyan/40 text-neon-cyan bg-neon-cyan/10"
                  : "border-neon-cyan/15 text-neon-cyan/60 bg-transparent hover:border-neon-cyan/30 hover:text-neon-cyan hover:bg-neon-cyan/5",
                "disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:border-neon-cyan/15 disabled:hover:bg-transparent disabled:hover:text-neon-cyan/60"
              )}
              aria-label="Copy API key"
            >
              {copiedKey ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              {copiedKey ? "Copied" : "Copy"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
