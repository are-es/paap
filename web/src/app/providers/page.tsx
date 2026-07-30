"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Provider } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  ProviderIcon,
  AuthTypeBadge,
  ProviderTypeBadge,
  StatusPill,
  providerNeonColor,
  NEON_BORDER_HOVER,
  NEON_ACCENT_STRIP,
} from "@/components/providers/provider-helpers";
import { Plus, Trash2, Key, Cpu, X } from "lucide-react";
import { cn } from "@/lib/utils";

export default function ProvidersPage() {
  const [addOpen, setAddOpen] = useState(false);

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => api.getProviders(),
  });

  const providers = providersQuery.data ?? [];

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center justify-between mb-6">
        <h1 className="font-heading text-2xl font-bold">Providers</h1>
        <Button onClick={() => setAddOpen(true)}>
          <Plus className="w-4 h-4 mr-1" /> Add Provider
        </Button>
      </div>

      {providersQuery.isLoading && (
        <div className="grid gap-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))" }}>
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="h-44 rounded-xl border border-border bg-card animate-pulse"
            />
          ))}
        </div>
      )}

      {providersQuery.isError && (
        <div className="rounded-xl border border-neon-magenta/20 bg-neon-magenta/5 p-4 text-sm text-neon-magenta">
          Failed to load providers: {(providersQuery.error as Error).message}
        </div>
      )}

      {!providersQuery.isLoading && providers.length === 0 && (
        <div className="rounded-xl border border-dashed border-border bg-card p-10 text-center">
          <p className="text-sm text-muted-foreground">No providers configured yet.</p>
          <Button className="mt-4" onClick={() => setAddOpen(true)}>
            <Plus className="w-4 h-4 mr-1" /> Add Provider
          </Button>
        </div>
      )}

      <div className="grid gap-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))" }}>
        {providers.map((provider) => (
          <ProviderCard key={provider.id} provider={provider} />
        ))}
      </div>

      <AddProviderModal open={addOpen} onClose={() => setAddOpen(false)} />
    </div>
  );
}

function ProviderCard({ provider }: { provider: Provider }) {
  const queryClient = useQueryClient();
  const neon = providerNeonColor(provider.name);

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteProvider(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["providers"] }),
  });

  const totalKeys = provider.key_count ?? provider.connection_count ?? 0;
  const activeKeys = provider.active_key_count ?? 0;
  const countLabel = `${activeKeys}/${totalKeys}`;

  const handleClick = () => {
    window.location.href = `/providers/${provider.id}`;
  };

  return (
    <div
      onClick={handleClick}
      className={cn(
        "group relative rounded-xl border border-border bg-card p-5 cursor-pointer transition-all duration-200 flex flex-col gap-4",
        "before:absolute before:top-0 before:left-0 before:bottom-0 before:w-[3px] before:rounded-l-xl before:opacity-0 before:transition-opacity",
        NEON_ACCENT_STRIP[neon],
        "before:group-hover:opacity-100",
        NEON_BORDER_HOVER[neon],
        "hover:translate-y-[-2px]"
      )}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <ProviderIcon provider={provider} size="md" />
          <h2 className="font-heading text-base font-semibold">{provider.name}</h2>
        </div>
        <div className="flex items-center gap-1.5">
          <AuthTypeBadge authType={provider.auth_type} />
          <ProviderTypeBadge providerType={provider.provider_type} />
        </div>
      </div>

      <p className="text-xs text-muted-foreground truncate">{provider.base_url}</p>

      <div className="flex items-center justify-between">
        <StatusPill status={provider.status} />
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span className="flex items-center gap-1">
            <Key className="w-3 h-3" />
            {countLabel}
          </span>
          <span className="flex items-center gap-1">
            <Cpu className="w-3 h-3" />
            {provider.model_count ?? 0}
          </span>
        </div>
      </div>

      {provider.provider_type !== "builtin" && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            deleteMutation.mutate(provider.id);
          }}
          className="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded hover:bg-destructive/10"
        >
          <X className="w-4 h-4 text-muted-foreground hover:text-destructive" />
        </button>
      )}
    </div>
  );
}

function AddProviderModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card border border-border rounded-xl p-6 w-full max-w-md">
        <h2 className="font-heading text-lg font-bold mb-4">Add Provider</h2>
        <p className="text-sm text-muted-foreground mb-4">
          Provider setup coming soon. For now, use the built-in providers.
        </p>
        <Button onClick={onClose} className="w-full">
          Close
        </Button>
      </div>
    </div>
  );
}
