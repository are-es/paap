"use client";

import { useQuery } from "@tanstack/react-query";
import { api, type Provider } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  ProviderIcon,
  AuthTypeBadge,
  ProviderTypeBadge,
  providerNeonColor,
  getNeonClasses,
} from "@/components/providers/provider-helpers";
import { Plus, Layers, KeyRound } from "lucide-react";
import { cn } from "@/lib/utils";
import { AddProviderModal } from "@/components/providers/add-provider-modal";
import { useState } from "react";

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
        <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(420px, 1fr))" }}>
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="h-24 rounded-lg border border-primary/15 bg-primary/[0.04] animate-pulse"
            />
          ))}
        </div>
      )}

      {providersQuery.isError && (
        <div className="rounded-lg border border-neon-magenta/20 bg-neon-magenta/5 p-4 text-sm text-neon-magenta">
          Failed to load providers: {(providersQuery.error as Error).message}
        </div>
      )}

      {!providersQuery.isLoading && providers.length === 0 && (
        <div className="rounded-lg border border-dashed border-primary/15 bg-primary/[0.04] p-10 text-center">
          <p className="text-sm text-muted-foreground">No providers configured yet.</p>
          <Button className="mt-4" onClick={() => setAddOpen(true)}>
            <Plus className="w-4 h-4 mr-1" /> Add Provider
          </Button>
        </div>
      )}

      <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(420px, 1fr))" }}>
        {providers.map((provider) => (
          <ProviderCard key={provider.id} provider={provider} />
        ))}
      </div>

      <AddProviderModal open={addOpen} onClose={() => setAddOpen(false)} />
    </div>
  );
}

function ProviderCard({ provider }: { provider: Provider }) {
  const neon = providerNeonColor(provider.name);

  const totalKeys = provider.key_count ?? provider.connection_count ?? 0;
  const activeKeys = provider.active_key_count ?? 0;
  const countLabel = `${activeKeys}/${totalKeys}`;

  const handleClick = () => {
    window.location.href = `/providers/setup?id=${encodeURIComponent(String(provider.id))}`;
  };

  const neonClasses = getNeonClasses(neon);

  return (
    <div
      onClick={handleClick}
      className={cn(
        "group relative flex rounded-lg border border-border bg-card overflow-hidden cursor-pointer transition-all duration-200",
        neonClasses.hover,
        "hover:shadow-lg hover:shadow-primary/5 hover:-translate-y-0.5"
      )}
      style={{ "--glow-color": `${neon}30` } as React.CSSProperties}
    >
      {/* Color stripe */}
      <div className="w-1 shrink-0" style={{ background: neon }} />

      {/* Content */}
      <div className="flex items-start gap-3 p-4 flex-1 min-w-0">
        <ProviderIcon provider={provider} size="lg" />

        <div className="flex-1 min-w-0">
          {/* Top row: name left, badges right */}
          <div className="flex items-start justify-between gap-2 mb-2">
            <h2 className="font-heading text-sm font-bold whitespace-nowrap mt-px">{provider.name}</h2>
            <div className="flex items-center gap-1.5 shrink-0">
              <ProviderTypeBadge providerType={provider.provider_type} />
              <AuthTypeBadge authType={provider.auth_type} />
              <span className={cn("w-1.5 h-1.5 rounded-full shrink-0", provider.status === "online" ? "bg-emerald-400/70" : "bg-zinc-500/60")} />
            </div>
          </div>

          {/* Stats row */}
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Layers className="h-3 w-3" />
              <span className="text-foreground font-medium">{provider.model_count ?? 0}</span> models
            </div>
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <KeyRound className="h-3 w-3" />
              <span className="text-foreground font-medium">{countLabel}</span> keys
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
