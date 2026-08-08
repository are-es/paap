"use client";

import { useState, useMemo } from "react";
import { api } from "@/lib/api";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";

interface AddProviderModalProps {
  open: boolean;
  onClose: () => void;
}

function inferNameFromUrl(url: string): string {
  if (!url) return "";
  try {
    const u = new URL(url);
    let host = u.hostname.replace(/^www\./, "");
    // strip common prefixes
    host = host.replace(/^(api|llm|us-central1-aiplatform|generativelanguage)\./, "");
    // second-level domain
    const parts = host.split(".");
    if (parts.length >= 2) {
      // handle googleapis.com -> google
      if (host.includes("google")) return "Google";
      if (host.includes("openai")) return "OpenAI";
      if (host.includes("anthropic")) return "Anthropic";
      if (host.includes("groq")) return "Groq";
      if (host.includes("xiaomi") || host.includes("mimo")) return "Xiaomi";
      if (host.includes("kimchi")) return "Kimchi";
      if (host.includes("meta")) return "Meta";
      if (host.includes("deepseek")) return "DeepSeek";
      if (host.includes("runapi")) return "RunAPI";
      if (host.includes("camel")) return "Camel";
      if (host.includes("apiview")) return "APIView";
      if (host.includes("tokenrouter")) return "TokenRouter";
      if (host.includes("ollama")) return "Ollama";
      // fallback: first label
      const label = parts[parts.length - 2] || parts[0];
      return label.charAt(0).toUpperCase() + label.slice(1);
    }
    return host.charAt(0).toUpperCase() + host.slice(1);
  } catch {
    return "";
  }
}

export function AddProviderModal({ open, onClose }: AddProviderModalProps) {
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [customHeaderValue, setCustomHeaderValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [nameTouched, setNameTouched] = useState(false);

  const queryClient = useQueryClient();

  const addMutation = useMutation({
    mutationFn: () =>
      api.addProvider({
        name: name || inferNameFromUrl(baseUrl) || "Custom Provider",
        base_url: baseUrl || "https://api.example.com",
        auth_type: "apikey",
        custom_headers: customHeaderValue ? JSON.stringify({"X-Custom-Header": customHeaderValue}) : undefined,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["providers"] });
      reset();
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "Failed to add provider"),
  });

  if (!open) return null;

  function reset() {
    setName("");
    setBaseUrl("");
    setCustomHeaderValue("");
    setError(null);
    setNameTouched(false);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!baseUrl.trim()) {
      setError("Base URL is required");
      return;
    }
    addMutation.mutate();
  }

  function handleUrlChange(v: string) {
    setBaseUrl(v);
    if (!nameTouched) {
      const inferred = inferNameFromUrl(v);
      if (inferred) setName(inferred);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-provider-title"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md bg-popover border border-border rounded-xl p-5 shadow-2xl">
        <div className="flex items-center justify-between mb-4">
          <h2 id="add-provider-title" className="font-heading text-lg font-bold text-neon-cyan">
            Add Provider
          </h2>
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-muted text-muted-foreground"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="provider-url" className="block text-xs font-medium mb-1.5 text-muted-foreground">
              Base URL
            </label>
            <input
              id="provider-url"
              type="text"
              value={baseUrl}
              onChange={(e) => handleUrlChange(e.target.value)}
              placeholder="https://api.openai.com/v1"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background font-mono focus:outline-none focus:border-neon-cyan/50 focus:shadow-[0_0_8px_rgba(0,240,255,0.1)]"
              autoFocus
            />
            {baseUrl && !nameTouched && (
              <p className="text-[10px] text-muted-foreground mt-1">Auto name: {inferNameFromUrl(baseUrl) || "—"}</p>
            )}
          </div>

          <div>
            <label htmlFor="provider-name" className="block text-xs font-medium mb-1.5 text-muted-foreground">
              Name { !nameTouched && <span className="text-[10px] opacity-60">(auto from URL, editable)</span>}
            </label>
            <input
              id="provider-name"
              type="text"
              value={name}
              onChange={(e) => { setName(e.target.value); setNameTouched(true); }}
              onFocus={() => setNameTouched(true)}
              placeholder="My Custom Provider"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background focus:outline-none focus:border-neon-cyan/50"
            />
          </div>

          <div>
            <label htmlFor="provider-header" className="block text-xs font-medium mb-1.5 text-muted-foreground">
              Custom Header <span className="text-[10px] opacity-60">(optional)</span>
            </label>
            <input
              id="provider-header"
              type="text"
              value={customHeaderValue}
              onChange={(e) => setCustomHeaderValue(e.target.value)}
              placeholder="X-Custom-Header value"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background font-mono focus:outline-none focus:border-neon-cyan/50"
            />
            <p className="text-[10px] text-muted-foreground mt-1">Sent as X-Custom-Header with every request to this provider</p>
          </div>

          {error && <p className="text-xs text-neon-magenta">{error}</p>}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 py-1.5 text-sm rounded-lg border border-border hover:bg-muted text-foreground"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={addMutation.isPending}
              className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground font-medium disabled:opacity-50 transition-shadow"
            >
              {addMutation.isPending ? "Adding..." : "Add Provider"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
