"use client";

import { useQuery } from "@tanstack/react-query";
import { api, type Provider } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  ProviderIcon,
  AuthTypeBadge,
  ProviderTypeBadge,
} from "@/components/providers/provider-helpers";
import { Plus, Layers, KeyRound } from "lucide-react";
import { cn } from "@/lib/utils";
import { AddProviderModal } from "@/components/providers/add-provider-modal";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { useState } from "react";
import { useLanguage } from "@/lib/language-context";

export default function ProvidersPage() {
  const [addOpen, setAddOpen] = useState(false);
  const [showDocs, setShowDocs] = useState(false);
  const [filter, setFilter] = useState<"all" | "builtin" | "custom">("all");
  const { t } = useLanguage();

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => api.getProviders(),
  });

  const providers = providersQuery.data ?? [];
  const builtin = providers.filter((p) => p.provider_type === "builtin");
  const custom = providers.filter((p) => p.provider_type !== "builtin");

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center justify-between mb-6">
        <h1 className="font-heading text-[30px] font-bold tracking-tight">Providers</h1>
        <div className="flex items-center gap-2">
          <DocsButton onClick={() => setShowDocs(true)} />
          <Button onClick={() => setAddOpen(true)}>
            <Plus className="w-4 h-4 mr-1" /> Add Provider
          </Button>
        </div>
      </div>

      {providersQuery.isLoading && (
        <div className="grid gap-2.5" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(290px, 1fr))" }}>
          {Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className="h-20 rounded-lg border border-border bg-muted/30 animate-pulse"
            />
          ))}
        </div>
      )}

      {providersQuery.isError && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">
          Failed to load providers: {(providersQuery.error as Error).message}
        </div>
      )}

      {!providersQuery.isLoading && providers.length === 0 && (
        <div className="rounded-lg border border-dashed border-border bg-muted/20 p-10 text-center">
          <p className="text-sm text-muted-foreground">No providers configured yet.</p>
          <Button className="mt-4" onClick={() => setAddOpen(true)}>
            <Plus className="w-4 h-4 mr-1" /> Add Provider
          </Button>
        </div>
      )}

      {/* Filter tabs */}
      <div className="flex items-center gap-0 mb-4 border-b border-border">
        {([["all", "All"], ["builtin", "Built-in"], ["custom", "Custom"]] as const).map(([key, label]) => (
          <button
            key={key}
            onClick={() => setFilter(key)}
            className={cn(
              "px-3 py-2 text-sm transition-colors -mb-px",
              filter === key
                ? "text-foreground font-medium border-b-2 border-foreground"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {filter === "all" ? (
        <div className="space-y-5">
          {builtin.length > 0 && (
            <div>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-2.5">
                Built-in ({builtin.length})
              </h2>
              <div className="grid gap-2.5" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(290px, 1fr))" }}>
                {builtin.map((p) => <ProviderCard key={p.id} provider={p} />)}
              </div>
            </div>
          )}
          {custom.length > 0 && (
            <div>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-2.5">
                Custom ({custom.length})
              </h2>
              <div className="grid gap-2.5" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(290px, 1fr))" }}>
                {custom.map((p) => <ProviderCard key={p.id} provider={p} />)}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div className="grid gap-2.5" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(290px, 1fr))" }}>
          {(filter === "builtin" ? builtin : custom).map((p) => (
            <ProviderCard key={p.id} provider={p} />
          ))}
        </div>
      )}

      <AddProviderModal open={addOpen} onClose={() => setAddOpen(false)} />

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title={t("providers_docs_title")}
        sections={[
          { title: t("providers_docs_overview_title"), content: t("providers_docs_overview_content") },
          { title: t("providers_docs_setup_title"), content: t("providers_docs_setup_content") },
          { title: t("providers_docs_keys_title"), content: t("providers_docs_keys_content") },
          { title: t("providers_docs_models_title"), content: t("providers_docs_models_content") },
          { title: t("providers_docs_playground_title"), content: t("providers_docs_playground_content") },
          { title: t("providers_docs_troubleshoot_title"), content: t("providers_docs_troubleshoot_content") },
        ]}
      />
    </div>
  );
}

function ProviderCard({ provider }: { provider: Provider }) {
  const totalKeys = provider.key_count ?? provider.connection_count ?? 0;
  const activeKeys = provider.active_key_count ?? 0;
  const countLabel = `${activeKeys}/${totalKeys}`;

  const handleClick = () => {
    window.location.href = `/providers/setup?id=${encodeURIComponent(String(provider.id))}`;
  };

  return (
    <div
      onClick={handleClick}
      className="group relative flex rounded-lg border border-border bg-card overflow-hidden cursor-pointer transition-colors hover:border-primary/40"
    >
      {/* Content */}
      <div className="flex items-center gap-3 p-3 flex-1 min-w-0">
        <ProviderIcon provider={provider} size="sm" />

        <div className="flex-1 min-w-0">
          {/* Top row: name left, badges right */}
          <div className="flex items-center justify-between gap-1.5 mb-1">
            <h2 className="font-heading text-xs font-bold truncate leading-tight">{provider.name}</h2>
            <div className="flex items-center gap-1 shrink-0">
              <ProviderTypeBadge providerType={provider.provider_type} />
              <AuthTypeBadge authType={provider.auth_type} />
              <span className={cn("w-1.5 h-1.5 rounded-full shrink-0", provider.status === "online" ? "bg-emerald-400/80" : "bg-zinc-500/60")} />
            </div>
          </div>

          {/* Stats row */}
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
              <Layers className="h-2.5 w-2.5" />
              <span className="text-foreground font-medium">{provider.model_count ?? 0}</span> models
            </div>
            <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
              <KeyRound className="h-2.5 w-2.5" />
              <span className="text-foreground font-medium">{countLabel}</span> keys
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
