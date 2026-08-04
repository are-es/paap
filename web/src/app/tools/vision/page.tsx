"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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

export default function VisionSetupPage() {
  const queryClient = useQueryClient();

  const toolsQuery = useQuery({
    queryKey: ["tools"],
    queryFn: () => api.getTools(),
  });

  const modelsQuery = useQuery({
    queryKey: ["allModels"],
    queryFn: () => api.getAllModels(),
  });

  const tools: any[] = toolsQuery.data || [];
  const visionTool = tools.find((t) => t.type === "vision");
  const models: { id: string; model_id: string; provider_id: string; provider_name: string }[] =
    modelsQuery.data || [];

  // routeModels is the ordered list of selected model strings
  const [routeModels, setRouteModels] = useState<string[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [saving, setSaving] = useState(false);
  const [initialized, setInitialized] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [dragIndex, setDragIndex] = useState<number | null>(null);

  // Initialize state from tool data
  useEffect(() => {
    if (visionTool && !initialized) {
      // route_model is now stored as JSON array string
      try {
        const parsed = JSON.parse(visionTool.route_model);
        if (Array.isArray(parsed)) {
          setRouteModels(parsed);
        } else {
          setRouteModels([visionTool.route_model]);
        }
      } catch {
        // Legacy single-string format
        if (visionTool.route_model) {
          setRouteModels([visionTool.route_model]);
        }
      }
      setEnabled(visionTool.enabled);
      setInitialized(true);
    }
  }, [visionTool, initialized]);

  const handleToggle = async () => {
    const newEnabled = !enabled;
    setEnabled(newEnabled);
    setSaving(true);
    try {
      if (visionTool) {
        await api.updateTool(visionTool.id, {
          ...visionTool,
          enabled: newEnabled,
        });
        queryClient.invalidateQueries({ queryKey: ["tools"] });
      }
    } catch {
      setEnabled(!newEnabled);
    } finally {
      setSaving(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      // Backend stores route_model as JSON array string
      const routeModelJson = JSON.stringify(routeModels);
      if (visionTool) {
        await api.updateTool(visionTool.id, {
          ...visionTool,
          route_model: routeModelJson,
        });
      } else {
        await api.createTool({
          name: "Vision Auto-Route",
          type: "vision",
          route_model: routeModelJson,
          enabled: true,
          priority: 10,
          config: "{}",
        });
      }
      queryClient.invalidateQueries({ queryKey: ["tools"] });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!visionTool) return;
    setConfirmDelete(false);
    await api.deleteTool(visionTool.id);
    setInitialized(false);
    queryClient.invalidateQueries({ queryKey: ["tools"] });
  };

  const addModel = (modelValue: string) => {
    if (modelValue && !routeModels.includes(modelValue)) {
      setRouteModels([...routeModels, modelValue]);
    }
  };

  const removeModel = (index: number) => {
    setRouteModels(routeModels.filter((_, i) => i !== index));
  };

  const handleDragStart = (index: number) => {
    setDragIndex(index);
  };

  const handleDragOver = useCallback(
    (e: React.DragEvent, index: number) => {
      e.preventDefault();
      if (dragIndex === null || dragIndex === index) return;
      const updated = [...routeModels];
      const [moved] = updated.splice(dragIndex, 1);
      updated.splice(index, 0, moved);
      setRouteModels(updated);
      setDragIndex(index);
    },
    [dragIndex, routeModels]
  );

  const handleDragEnd = () => {
    setDragIndex(null);
  };

  // Available models not yet selected
  const availableModels = models.filter(
    (m) => !routeModels.includes(`${m.provider_id}/${m.model_id}`)
  );

  if (toolsQuery.isLoading) {
    return (
      <div className="p-6 md:p-8 min-h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/tools" className="hover:text-foreground transition-colors">
          Tools
        </Link>
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
            <p className="text-xs text-muted-foreground">
              Auto-route images to vision-capable models with fallback chain
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          {visionTool && (
            <>
              <button
                onClick={() => setConfirmDelete(true)}
                className="p-2 rounded-lg text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors"
                title="Delete"
              >
                <Trash2 className="w-4 h-4" />
              </button>
              <button
                onClick={handleToggle}
                disabled={saving}
                className={cn(
                  "w-9 h-5 rounded-full relative transition-colors shrink-0",
                  enabled ? "bg-primary" : "bg-muted"
                )}
              >
                <span
                  className={cn(
                    "absolute top-0.5 w-4 h-4 rounded-full bg-background shadow transition-transform",
                    enabled ? "left-[18px]" : "left-0.5"
                  )}
                />
              </button>
            </>
          )}
        </div>
      </div>

      {/* Settings Card */}
      <div className="border border-border rounded-xl bg-card overflow-hidden">
        <div className="p-4 border-b border-border">
          <h2 className="text-sm font-medium">Configuration</h2>
        </div>

        <div className="p-4 space-y-4">
          {/* Status */}
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">Status</p>
              <p className="text-xs text-muted-foreground">
                {enabled ? "Active — images auto-routed to vision models" : "Disabled"}
              </p>
            </div>
            <div className={cn(
              "px-2 py-0.5 rounded-full text-[10px] font-medium",
              enabled
                ? "bg-green-500/10 text-green-500"
                : "bg-muted text-muted-foreground"
            )}>
              {enabled ? "ON" : "OFF"}
            </div>
          </div>

          {/* Fallback Chain */}
          <div>
            <label className="text-xs font-medium text-muted-foreground mb-1.5 block">
              Fallback Chain
              <span className="ml-2 text-[10px] font-normal">
                {routeModels.length} model{routeModels.length !== 1 ? "s" : ""} — drag to reorder
              </span>
            </label>

            {/* Selected models list */}
            {routeModels.length > 0 && (
              <div className="space-y-1 mb-2">
                {routeModels.map((model, index) => (
                  <div
                    key={model}
                    draggable
                    onDragStart={() => handleDragStart(index)}
                    onDragOver={(e) => handleDragOver(e, index)}
                    onDragEnd={handleDragEnd}
                    className={cn(
                      "flex items-center gap-2 px-2 py-1.5 rounded-lg border border-input bg-background text-sm font-mono",
                      dragIndex === index ? "opacity-50" : "",
                      "cursor-grab active:cursor-grabbing"
                    )}
                  >
                    <GripVertical className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                    <span className="text-[10px] text-muted-foreground w-4 shrink-0">
                      {index + 1}.
                    </span>
                    <span className="flex-1 truncate">{model}</span>
                    <button
                      onClick={() => removeModel(index)}
                      className="p-0.5 rounded hover:bg-destructive/10 hover:text-destructive transition-colors shrink-0"
                    >
                      <X className="w-3.5 h-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* Add model dropdown */}
            {availableModels.length > 0 && (
              <div className="flex gap-2">
                <select
                  id="add-model-select"
                  defaultValue=""
                  className="flex-1 px-3 py-2 text-sm rounded-lg border border-input bg-background font-mono focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="" disabled>
                    Add model to chain...
                  </option>
                  {availableModels.map((m) => (
                    <option key={m.id} value={`${m.provider_id}/${m.model_id}`}>
                      {m.provider_name} / {m.model_id}
                    </option>
                  ))}
                </select>
                <button
                  onClick={() => {
                    const sel = document.getElementById("add-model-select") as HTMLSelectElement;
                    if (sel?.value) {
                      addModel(sel.value);
                      sel.value = "";
                    }
                  }}
                  className="px-3 py-2 rounded-lg border border-input hover:bg-accent transition-colors"
                >
                  <Plus className="w-4 h-4" />
                </button>
              </div>
            )}

            <p className="text-[10px] text-muted-foreground mt-1.5">
              First model tried first. If it fails (no keys, error), next model in chain is used.
            </p>
          </div>

          {/* Save */}
          <div className="flex justify-end pt-2">
            <button
              onClick={handleSave}
              disabled={saving || routeModels.length === 0}
              className="px-4 py-2 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {saving ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                "Simpan"
              )}
            </button>
          </div>
        </div>
      </div>

      {/* Info */}
      <div className="mt-6 p-4 rounded-xl border border-border bg-card">
        <h3 className="text-sm font-medium mb-2">How It Works</h3>
        <ul className="text-xs text-muted-foreground space-y-1.5">
          <li>1. Client sends request with images (base64 or URL)</li>
          <li>2. PAAP detects images in request</li>
          <li>3. PAAP tries first model in fallback chain</li>
          <li>4. If first model fails (no active keys, error), tries next model</li>
          <li>5. Images sent intact — not converted to text descriptions</li>
        </ul>
      </div>

      <ConfirmModal
        open={confirmDelete}
        title="Delete Vision Tool"
        message="Hapus Vision tool? Ini tidak bisa dibatalkan."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={handleDelete}
        onCancel={() => setConfirmDelete(false)}
      />
    </div>
  );
}
