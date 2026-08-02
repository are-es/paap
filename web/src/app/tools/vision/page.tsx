"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  ChevronRight,
  Eye,
  Loader2,
  Trash2,
} from "lucide-react";
import { cn } from "@/lib/utils";

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

  const [routeModel, setRouteModel] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [saving, setSaving] = useState(false);
  const [initialized, setInitialized] = useState(false);

  // Initialize state from tool data
  useEffect(() => {
    if (visionTool && !initialized) {
      setRouteModel(visionTool.route_model);
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
      setEnabled(!newEnabled); // rollback
    } finally {
      setSaving(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      if (visionTool) {
        await api.updateTool(visionTool.id, {
          ...visionTool,
          route_model: routeModel,
        });
      } else {
        await api.createTool({
          name: "Vision Auto-Route",
          type: "vision",
          route_model: routeModel,
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
    if (!confirm("Hapus Vision tool?")) return;
    await api.deleteTool(visionTool.id);
    setInitialized(false);
    queryClient.invalidateQueries({ queryKey: ["tools"] });
  };

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
              Auto-route gambar ke model yang support vision
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          {visionTool && (
            <>
              <button
                onClick={handleDelete}
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
                {enabled ? "Active — gambar akan di-route otomatis" : "Disabled"}
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

          {/* Route Model */}
          <div>
            <label className="text-xs font-medium text-muted-foreground mb-1.5 block">
              Route Model
            </label>
            <select
              value={routeModel}
              onChange={(e) => setRouteModel(e.target.value)}
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background font-mono focus:outline-none focus:ring-1 focus:ring-ring"
            >
              {models.map((m) => (
                <option key={m.id} value={`${m.provider_id}/${m.model_id}`}>
                  {m.provider_name} / {m.model_id}
                </option>
              ))}
            </select>
            <p className="text-[10px] text-muted-foreground mt-1.5">
              Model ini akan dipakai otomatis saat request mengandung gambar
            </p>
          </div>

          {/* Save */}
          <div className="flex justify-end pt-2">
            <button
              onClick={handleSave}
              disabled={saving || !routeModel}
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
        <h3 className="text-sm font-medium mb-2">Cara Kerja</h3>
        <ul className="text-xs text-muted-foreground space-y-1.5">
          <li>1. Client kirim request dengan gambar (base64 atau URL)</li>
          <li>2. PAAP detect ada gambar di request</li>
          <li>3. PAAP auto-switch model ke yang lo pilih di atas</li>
          <li>4. Gambar tetap dikirim utuh — gak di-describe jadi text</li>
          <li>5. Model vision proses gambar langsung</li>
        </ul>
      </div>
    </div>
  );
}
