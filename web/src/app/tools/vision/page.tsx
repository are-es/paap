"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  ChevronRight,
  Eye,
  Loader2,
  Trash2,
  GripVertical,
  X,
  Plus,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { ConfirmModal } from "@/components/ui/confirm-modal";
import { DocsModal, DocsButton } from "@/components/ui/docs-modal";
import { useLanguage } from "@/lib/language-context";

// Add Model Modal — same pattern as Groups
function AddModelModal({ open, onClose, providers, existingModels, onAdd }: {
  open: boolean;
  onClose: () => void;
  providers: any[];
  existingModels: string[];
  onAdd: (model: string) => void;
}) {
  const [selectedProvider, setSelectedProvider] = useState("");
  const [selectedModel, setSelectedModel] = useState("");

  const modelsQuery = useQuery({
    queryKey: ["models", selectedProvider],
    queryFn: () => api.getModels(selectedProvider),
    enabled: !!selectedProvider && open,
  });
  const providerModels: any[] = modelsQuery.data || [];
  const availableModels = providerModels.filter((m: any) => !existingModels.includes(`${selectedProvider}/${m.name}`));

  const handleAdd = () => {
    if (!selectedProvider || !selectedModel) return;
    onAdd(`${selectedProvider}/${selectedModel}`);
    setSelectedProvider("");
    setSelectedModel("");
    onClose();
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative bg-card border border-border rounded-xl shadow-xl w-[400px] max-h-[80vh] overflow-y-auto">
        <div className="p-4 border-b border-border">
          <h3 className="text-sm font-semibold">Add Vision Model</h3>
          <p className="text-xs text-muted-foreground mt-1">Select provider and model to add to fallback chain</p>
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
              {availableModels.map((m: any) => <option key={m.id} value={m.name}>{m.name}</option>)}
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

export default function VisionSetupPage() {
  const queryClient = useQueryClient();
  const { t } = useLanguage();

  const toolsQuery = useQuery({ queryKey: ["tools"], queryFn: () => api.getTools() });
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: () => api.getProviders() });

  const tools: any[] = toolsQuery.data || [];
  const visionTool = tools.find((t: any) => t.type === "vision");
  const providers: any[] = providersQuery.data || [];

  const [routeModels, setRouteModels] = useState<string[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [initialized, setInitialized] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showDocs, setShowDocs] = useState(false);

  // Initialize from tool data
  useEffect(() => {
    if (visionTool && !initialized) {
      try {
        const parsed = JSON.parse(visionTool.route_model);
        if (Array.isArray(parsed)) setRouteModels(parsed);
        else setRouteModels([visionTool.route_model]);
      } catch {
        if (visionTool.route_model) setRouteModels([visionTool.route_model]);
      }
      setEnabled(visionTool.enabled);
      setInitialized(true);
    }
  }, [visionTool, initialized]);

  // Save mutation — explicit save to API
  const saveModels = useCallback(async (models: string[]) => {
    const routeModelJson = JSON.stringify(models);
    if (visionTool) {
      await api.updateTool(visionTool.id, { ...visionTool, route_model: routeModelJson });
    } else {
      await api.createTool({ name: "Vision Auto-Route", type: "vision", route_model: routeModelJson, enabled: true, priority: 10, config: "{}" });
    }
    queryClient.invalidateQueries({ queryKey: ["tools"] });
  }, [visionTool, queryClient]);

  const handleToggle = async () => {
    const newEnabled = !enabled;
    setEnabled(newEnabled);
    try {
      if (visionTool) {
        await api.updateTool(visionTool.id, { ...visionTool, enabled: newEnabled });
        queryClient.invalidateQueries({ queryKey: ["tools"] });
      }
    } catch { setEnabled(!newEnabled); }
  };

  const handleDelete = async () => {
    if (!visionTool) return;
    setConfirmDelete(false);
    await api.deleteTool(visionTool.id);
    setInitialized(false);
    queryClient.invalidateQueries({ queryKey: ["tools"] });
  };

  // Add model from modal — auto-save
  const addModel = async (modelValue: string) => {
    if (routeModels.includes(modelValue)) return;
    const updated = [...routeModels, modelValue];
    setRouteModels(updated);
    await saveModels(updated);
  };

  // Remove model — auto-save
  const removeModel = async (index: number) => {
    const updated = routeModels.filter((_, i) => i !== index);
    setRouteModels(updated);
    await saveModels(updated);
  };

  // Drag to reorder + auto-save
  const handleDragStart = (index: number) => setDragIndex(index);
  const handleDragOver = useCallback((e: React.DragEvent, index: number) => {
    e.preventDefault();
    if (dragIndex === null || dragIndex === index) return;
    const updated = [...routeModels];
    const [moved] = updated.splice(dragIndex, 1);
    updated.splice(index, 0, moved);
    setRouteModels(updated);
    setDragIndex(index);
  }, [dragIndex, routeModels]);
  const handleDragEnd = async () => {
    if (dragIndex !== null) await saveModels(routeModels);
    setDragIndex(null);
  };

  if (toolsQuery.isLoading) {
    return <div className="p-6 md:p-8 min-h-full flex items-center justify-center"><Loader2 className="w-6 h-6 animate-spin text-muted-foreground" /></div>;
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/tools" className="hover:text-foreground transition-colors">Tools</Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">Vision</span>
      </nav>

      {/* Header */}
      <div className="flex items-center justify-between gap-4 mb-8">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-lg bg-primary/10 flex items-center justify-center">
            <Eye className="w-5 h-5 text-primary" />
          </div>
          <div>
            <h1 className="font-heading text-xl font-bold">Vision</h1>
            <p className="text-xs text-muted-foreground">Auto-route images to vision-capable models with fallback chain</p>
          </div>
        </div>
        <div className="flex items-center gap-3 shrink-0">
          {visionTool && (
            <>
              <DocsButton onClick={() => setShowDocs(true)} />
              <button onClick={() => setConfirmDelete(true)} className="p-2 rounded-lg text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors" title="Delete">
                <Trash2 className="w-4 h-4" />
              </button>
              <button onClick={handleToggle} className={cn("w-9 h-5 rounded-full relative transition-colors shrink-0", enabled ? "bg-primary" : "bg-muted")}>
                <span className={cn("absolute top-0.5 w-4 h-4 rounded-full bg-background shadow transition-transform", enabled ? "left-[18px]" : "left-0.5")} />
              </button>
            </>
          )}
        </div>
      </div>

      {/* Fallback Chain Card */}
      <div className="border border-border rounded-xl bg-card overflow-hidden">
        <div className="p-4 border-b border-border">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium">Fallback Chain</h2>
            <span className="text-[10px] text-muted-foreground">{routeModels.length} model{routeModels.length !== 1 ? "s" : ""}</span>
          </div>
          <p className="text-[10px] text-muted-foreground mt-1">First model tried first. If it fails, next model in chain is used. Drag to reorder.</p>
        </div>

        <div className="p-4 space-y-3">
          {/* Selected models */}
          {routeModels.length > 0 && (
            <div className="space-y-1">
              {routeModels.map((model, index) => {
                const [pid, mid] = model.split("/");
                const prov = providers.find((p: any) => p.id === pid);
                return (
                  <div
                    key={model}
                    draggable
                    onDragStart={() => handleDragStart(index)}
                    onDragOver={(e) => handleDragOver(e, index)}
                    onDragEnd={handleDragEnd}
                    className={cn("flex items-center gap-2 px-3 py-2 rounded-lg border border-input bg-background text-sm", dragIndex === index ? "opacity-50" : "", "cursor-grab active:cursor-grabbing")}
                  >
                    <GripVertical className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                    <span className="text-[10px] text-muted-foreground w-4 shrink-0">{index + 1}.</span>
                    <span className="text-xs text-muted-foreground shrink-0">{prov?.name || pid}</span>
                    <span className="text-muted-foreground shrink-0">/</span>
                    <span className="flex-1 truncate font-mono text-xs">{mid}</span>
                    <button onClick={() => removeModel(index)} className="p-0.5 rounded hover:bg-destructive/10 hover:text-destructive transition-colors shrink-0">
                      <X className="w-3.5 h-3.5" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          {/* Add button — opens modal */}
          <button
            onClick={() => setShowAddModal(true)}
            className="w-full px-3 py-2 text-sm text-muted-foreground rounded-lg border border-dashed border-input hover:border-primary hover:text-primary transition-colors flex items-center justify-center gap-2"
          >
            <Plus className="w-4 h-4" />
            Add Model
          </button>
        </div>
      </div>

      {/* Info */}
      <div className="mt-6 p-4 rounded-xl border border-border bg-card">
        <h3 className="text-sm font-medium mb-2">How It Works</h3>
        <ul className="text-xs text-muted-foreground space-y-1.5">
          <li>1. Client sends request with images (base64 or URL)</li>
          <li>2. PAAP detects images in request</li>
          <li>3. PAAP tries first model in fallback chain</li>
          <li>4. If first model fails (no keys, error), tries next model</li>
          <li>5. Images sent intact — not converted to text descriptions</li>
        </ul>
      </div>

      {/* Modals */}
      <AddModelModal
        open={showAddModal}
        onClose={() => setShowAddModal(false)}
        providers={providers}
        existingModels={routeModels}
        onAdd={addModel}
      />
      <ConfirmModal
        open={confirmDelete}
        title="Delete Vision Tool"
        message="Hapus Vision tool? Ini tidak bisa dibatalkan."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={handleDelete}
        onCancel={() => setConfirmDelete(false)}
      />

      <DocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        title={t("vision_docs_title")}
        sections={[
          { title: t("vision_docs_overview_title"), content: t("vision_docs_overview_content") },
          { title: t("vision_docs_setup_title"), content: t("vision_docs_setup_content") },
          { title: t("vision_docs_troubleshoot_title"), content: t("vision_docs_troubleshoot_content") },
        ]}
      />
    </div>
  );
}
