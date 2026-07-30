"use client";

import { useState } from "react";
import { api, type Provider } from "@/lib/api";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

interface AddProviderModalProps {
  open: boolean;
  onClose: () => void;
}

export function AddProviderModal({ open, onClose }: AddProviderModalProps) {
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [authType, setAuthType] = useState<Provider["auth_type"]>("apikey");
  const [error, setError] = useState<string | null>(null);

  const queryClient = useQueryClient();

  const addMutation = useMutation({
    mutationFn: () =>
      api.addProvider({ name, base_url: baseUrl || "https://api.example.com", auth_type: authType }),
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
    setAuthType("apikey");
    setError(null);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!name.trim()) {
      setError("Provider name is required");
      return;
    }
    addMutation.mutate();
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-provider-title"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md bg-card rounded-xl border border-border p-5">
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
            <label htmlFor="provider-name" className="block text-xs font-medium mb-1.5 text-muted-foreground">
              Name
            </label>
            <input
              id="provider-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Custom Provider"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background focus:outline-none focus:border-neon-cyan/50"
              autoFocus
            />
          </div>

          <div>
            <label htmlFor="provider-url" className="block text-xs font-medium mb-1.5 text-muted-foreground">
              Base URL
            </label>
            <input
              id="provider-url"
              type="text"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="https://api.example.com"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background font-mono focus:outline-none focus:border-neon-cyan/50 focus:shadow-[0_0_8px_rgba(0,240,255,0.1)]"
            />
          </div>

          <div>
            <span className="block text-xs font-medium mb-1.5 text-muted-foreground">Auth Type</span>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setAuthType("apikey")}
                className={cn(
                  "flex-1 px-3 py-2 text-xs rounded-lg border transition-all",
                  authType === "apikey"
                    ? "border-neon-cyan bg-neon-cyan/10 text-neon-cyan"
                    : "border-border bg-background hover:bg-muted text-muted-foreground"
                )}
              >
                API Key
              </button>
              <button
                type="button"
                onClick={() => setAuthType("connection")}
                className={cn(
                  "flex-1 px-3 py-2 text-xs rounded-lg border transition-all",
                  authType === "connection"
                    ? "border-neon-purple bg-neon-purple/10 text-neon-purple"
                    : "border-border bg-background hover:bg-muted text-muted-foreground"
                )}
              >
                Connection
              </button>
            </div>
          </div>

          {error && <p className="text-xs text-neon-magenta">{error}</p>}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 py-1.5 text-sm rounded-lg border border-border hover:bg-muted"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={addMutation.isPending}
              className="px-3 py-1.5 text-sm rounded-lg bg-neon-cyan text-[#0a0a14] font-medium disabled:opacity-50 transition-shadow"
            >
              {addMutation.isPending ? "Adding..." : "Add Provider"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
