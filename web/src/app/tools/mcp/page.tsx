"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useLanguage } from "@/lib/language-context";
import { ChevronRight, Wand2, Volume2, X, Loader2, Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";

function getMcpDocs(t: (key: string) => string) {
  return [
    {
      title: t("mcp_docs_overview_title"),
      content: t("mcp_docs_overview_content"),
    },
    {
      title: t("mcp_docs_setup_title"),
      content: t("mcp_docs_setup_content"),
    },
    {
      title: t("mcp_docs_usage_title"),
      content: t("mcp_docs_usage_content"),
    },
    {
      title: t("mcp_docs_troubleshoot_title"),
      content: t("mcp_docs_troubleshoot_content"),
    },
  ];
}

// Add Model Modal
function AddModelModal({ open, onClose, providers, existingModel, onAdd }: {
  open: boolean;
  onClose: () => void;
  providers: any[];
  existingModel: string;
  onAdd: (provider: string, model: string) => void;
}) {
  const [selectedProvider, setSelectedProvider] = useState("");
  const [selectedModel, setSelectedModel] = useState("");

  const modelsQuery = useQuery({
    queryKey: ["models", selectedProvider],
    queryFn: () => api.getModels(selectedProvider),
    enabled: !!selectedProvider && open,
  });
  const providerModels: any[] = modelsQuery.data || [];

  const handleAdd = () => {
    if (!selectedProvider || !selectedModel) return;
    onAdd(selectedProvider, selectedModel);
    setSelectedProvider("");
    setSelectedModel("");
    onClose();
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative bg-card border border-border rounded-xl shadow-xl w-[400px]">
        <div className="p-4 border-b border-border">
          <h3 className="text-sm font-semibold">Select Model</h3>
        </div>
        <div className="p-4 space-y-3">
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Provider</label>
            <select
              value={selectedProvider}
              onChange={(e) => { setSelectedProvider(e.target.value); setSelectedModel(""); }}
              className="w-full px-3 py-2 text-sm text-foreground rounded-lg border border-input bg-background focus:outline-none focus:ring-1 focus:ring-ring"
            >
              <option value="">Select provider...</option>
              {providers.map((p: any) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">Model</label>
            <select
              value={selectedModel}
              onChange={(e) => setSelectedModel(e.target.value)}
              disabled={!selectedProvider}
              className="w-full px-3 py-2 text-sm text-foreground rounded-lg border border-input bg-background focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            >
              <option value="">Select model...</option>
              {providerModels.map((m: any) => <option key={m.id} value={m.name}>{m.name}</option>)}
            </select>
          </div>
        </div>
        <div className="p-4 border-t border-border flex justify-end gap-2">
          <button onClick={onClose} className="px-3 py-1.5 text-sm rounded-lg border border-input hover:bg-accent transition-colors">Cancel</button>
          <button onClick={handleAdd} disabled={!selectedProvider || !selectedModel} className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:opacity-90 transition-colors disabled:opacity-50">Add</button>
        </div>
      </div>
    </div>
  );
}

// Model display with remove button
function ModelTag({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-mono bg-muted rounded-md">
      {label}
      <button onClick={onRemove} className="hover:text-destructive transition-colors">
        <X className="w-3 h-3" />
      </button>
    </span>
  );
}

export default function MCPToolsPage() {
  const queryClient = useQueryClient();
  const { t } = useLanguage();

  const settingsQuery = useQuery({ queryKey: ["settings"], queryFn: () => api.getSettings() });
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: () => api.getProviders() });
  const settings: any = settingsQuery.data || {};
  const providers: any[] = providersQuery.data || [];

  const [mcpEnabled, setMcpEnabled] = useState(false);
  const [imageGenProvider, setImageGenProvider] = useState("");
  const [imageGenModel, setImageGenModel] = useState("");
  const [ttsProvider, setTtsProvider] = useState("");
  const [ttsModel, setTtsModel] = useState("");
  const [initialized, setInitialized] = useState(false);

  const [showImageModal, setShowImageModal] = useState(false);
  const [showTtsModal, setShowTtsModal] = useState(false);
  const [showDocs, setShowDocs] = useState(false);

  const mcpDocs = getMcpDocs(t);

  useEffect(() => {
    if (settings && settings.mcp_enabled !== undefined && !initialized) {
      setMcpEnabled(settings.mcp_enabled === "true");
      setImageGenProvider(settings.mcp_image_provider || "");
      setImageGenModel(settings.mcp_image_model || "");
      setTtsProvider(settings.mcp_tts_provider || "");
      setTtsModel(settings.mcp_tts_model || "");
      setInitialized(true);
    }
  }, [settings, initialized]);

  const updateSettingMutation = useMutation({
    mutationFn: (data: any) => api.updateSettings(data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["settings"] }),
  });

  const handleToggle = async () => {
    const newVal = !mcpEnabled;
    setMcpEnabled(newVal);
    updateSettingMutation.mutate({ mcp_enabled: String(newVal) });
  };

  const handleAddImageModel = (provider: string, model: string) => {
    setImageGenProvider(provider);
    setImageGenModel(model);
    updateSettingMutation.mutate({ mcp_image_provider: provider, mcp_image_model: model });
  };

  const handleAddTtsModel = (provider: string, model: string) => {
    setTtsProvider(provider);
    setTtsModel(model);
    updateSettingMutation.mutate({ mcp_tts_provider: provider, mcp_tts_model: model });
  };

  const handleRemoveImageModel = () => {
    setImageGenProvider("");
    setImageGenModel("");
    updateSettingMutation.mutate({ mcp_image_provider: "", mcp_image_model: "" });
  };

  const handleRemoveTtsModel = () => {
    setTtsProvider("");
    setTtsModel("");
    updateSettingMutation.mutate({ mcp_tts_provider: "", mcp_tts_model: "" });
  };

  const providerName = (id: string) => providers.find((p: any) => p.id === id)?.name || id;

  if (settingsQuery.isLoading) {
    return <div className="p-6 md:p-8 min-h-full flex items-center justify-center"><Loader2 className="w-6 h-6 animate-spin text-muted-foreground" /></div>;
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/tools" className="hover:text-foreground transition-colors">{t("nav_tools")}</Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">MCP Tools</span>
      </nav>

      {/* Header */}
      <div className="flex items-center justify-between gap-4 mb-8">
        <div>
          <h1 className="font-heading text-xl font-bold">MCP Tools</h1>
          <p className="text-xs text-muted-foreground">{t("mcp_subtitle")}</p>
        </div>
        <div className="flex items-center gap-2">
          <DocsButton onClick={() => setShowDocs(true)} />
          <button onClick={handleToggle} className={cn("w-9 h-5 rounded-full relative transition-colors shrink-0", mcpEnabled ? "bg-primary" : "bg-muted")}>
            <span className={cn("absolute top-0.5 w-4 h-4 rounded-full bg-background shadow transition-transform", mcpEnabled ? "left-[18px]" : "left-0.5")} />
          </button>
        </div>
      </div>

      {/* 2 Vertical Cards */}
      <div className="space-y-4">
        {/* Image Gen Card */}
        <div className="border border-border rounded-xl bg-card p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-lg bg-primary/10 flex items-center justify-center">
                <Wand2 className="w-4 h-4 text-primary" />
              </div>
              <span className="text-sm font-medium">{t("mcp_image_gen")}</span>
            </div>
            <span className={cn("text-[10px] px-1.5 py-0.5 rounded", imageGenModel ? "bg-neon-green/10 text-neon-green" : "bg-muted text-muted-foreground")}>
              {imageGenModel ? t("status_on") : t("status_off")}
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
            {imageGenModel ? (
              <ModelTag label={`${providerName(imageGenProvider)}/${imageGenModel}`} onRemove={handleRemoveImageModel} />
            ) : (
              <span className="text-xs text-muted-foreground">{t("mcp_no_model")}</span>
            )}
            <button
              onClick={() => setShowImageModal(true)}
              className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded-md border border-dashed border-input hover:border-primary hover:text-primary transition-colors"
            >
              <Plus className="w-3 h-3" />
              {imageGenModel ? t("btn_change") : t("btn_add")}
            </button>
          </div>
        </div>

        {/* TTS Card */}
        <div className="border border-border rounded-xl bg-card p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-lg bg-neon-green/10 flex items-center justify-center">
                <Volume2 className="w-4 h-4 text-neon-green" />
              </div>
              <span className="text-sm font-medium">{t("mcp_tts")}</span>
            </div>
            <span className={cn("text-[10px] px-1.5 py-0.5 rounded", ttsModel ? "bg-neon-green/10 text-neon-green" : "bg-muted text-muted-foreground")}>
              {ttsModel ? t("status_on") : t("status_off")}
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
            {ttsModel ? (
              <ModelTag label={`${providerName(ttsProvider)}/${ttsModel}`} onRemove={handleRemoveTtsModel} />
            ) : (
              <span className="text-xs text-muted-foreground">{t("mcp_no_model")}</span>
            )}
            <button
              onClick={() => setShowTtsModal(true)}
              className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded-md border border-dashed border-input hover:border-primary hover:text-primary transition-colors"
            >
              <Plus className="w-3 h-3" />
              {ttsModel ? t("btn_change") : t("btn_add")}
            </button>
          </div>
        </div>
      </div>

      {/* Endpoints */}
      <div className="mt-6 border border-border rounded-xl bg-card p-4">
        <h3 className="text-xs font-medium text-foreground mb-3">Endpoints</h3>
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground">GET</span>
            <code className="text-xs text-muted-foreground">/mcp/status</code>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground">POST</span>
            <code className="text-xs text-muted-foreground">/mcp/message</code>
          </div>
        </div>
      </div>

      {/* Modals */}
      <AddModelModal open={showImageModal} onClose={() => setShowImageModal(false)} providers={providers} existingModel={imageGenModel} onAdd={handleAddImageModel} />
      <AddModelModal open={showTtsModal} onClose={() => setShowTtsModal(false)} providers={providers} existingModel={ttsModel} onAdd={handleAddTtsModel} />
      <DocsModal open={showDocs} onClose={() => setShowDocs(false)} title="MCP Tools" sections={mcpDocs} />
    </div>
  );
}
