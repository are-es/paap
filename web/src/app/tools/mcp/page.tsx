"use client";

import { useState, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { ChevronLeft, Globe, Image, Volume2, Eye, Check } from "lucide-react";
import Link from "next/link";
import { cn } from "@/lib/utils";

interface ModelItem { id: string; name?: string; }

export default function McpToolsPage() {
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState<string | null>(null);

  const settingsQuery = useQuery({ queryKey: ["settings"], queryFn: () => api.getSettings() });
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: () => api.getProviders() });
  const mcpStatusQuery = useQuery({ queryKey: ["mcp-status"], queryFn: () => fetch("/mcp/status").then((r) => r.json()) });

  const settings = settingsQuery.data;
  const providers = providersQuery.data ?? [];
  const mcpStatus = mcpStatusQuery.data;

  const [imageProvider, setImageProvider] = useState("");
  const [ttsProvider, setTtsProvider] = useState("");
  const [imageModels, setImageModels] = useState<ModelItem[]>([]);
  const [ttsModels, setTtsModels] = useState<ModelItem[]>([]);

  useEffect(() => {
    if (settings) {
      setImageProvider(settings.mcp_image_provider ? String(settings.mcp_image_provider) : "");
      setTtsProvider(settings.mcp_tts_provider ? String(settings.mcp_tts_provider) : "");
    }
  }, [settings]);

  useEffect(() => {
    if (!imageProvider) { setImageModels([]); return; }
    api.getModels(imageProvider).then((m) => setImageModels(m ?? [])).catch(() => setImageModels([]));
  }, [imageProvider]);

  useEffect(() => {
    if (!ttsProvider) { setTtsModels([]); return; }
    api.getModels(ttsProvider).then((m) => setTtsModels(m ?? [])).catch(() => setTtsModels([]));
  }, [ttsProvider]);

  const mcpEnabled = settings?.mcp_enabled !== "false";
  const imageModel = settings?.mcp_image_model ? String(settings.mcp_image_model) : "";
  const ttsModel = settings?.mcp_tts_model ? String(settings.mcp_tts_model) : "";
  const ttsVoice = settings?.mcp_tts_voice ? String(settings.mcp_tts_voice) : "alloy";

  const updateSetting = async (key: string, value: string) => {
    setSaving(key);
    await api.updateSettings({ [key]: value });
    queryClient.invalidateQueries({ queryKey: ["settings"] });
    setTimeout(() => setSaving(null), 500);
  };

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center gap-3 mb-7">
        <Link href="/tools" className="p-1.5 hover:bg-secondary rounded-lg transition-colors">
          <ChevronLeft className="w-5 h-5" />
        </Link>
        <div className="flex items-center gap-2">
          <Globe className="w-5 h-5 text-primary" />
          <h1 className="font-heading text-xl font-bold">MCP Tools</h1>
        </div>
      </div>

      {/* MCP Status */}
      <div className="mb-6 p-4 rounded-xl border border-border bg-card">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-medium">MCP Server</span>
          <button
            onClick={() => updateSetting("mcp_enabled", mcpEnabled ? "false" : "true")}
            className={cn("relative w-9 h-5 rounded-full transition-colors", mcpEnabled ? "bg-primary" : "bg-muted")}
          >
            <span className={cn("absolute top-0.5 w-4 h-4 rounded-full bg-background shadow transition-transform", mcpEnabled ? "left-[18px]" : "left-0.5")} />
          </button>
        </div>
        <p className="text-xs text-muted-foreground mb-3">
          MCP endpoint: <code className="text-primary">POST /mcp/message</code>
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Image Generation */}
        <div className="p-4 rounded-xl border border-border bg-card">
          <div className="flex items-center gap-2 mb-3">
            <Image className="w-4 h-4 text-primary" />
            <span className="text-sm font-semibold">Image Generation</span>
          </div>
          <p className="text-xs text-muted-foreground mb-3">Tool: <code>generate_image</code> — text → image</p>
          <div className="space-y-3">
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Provider</label>
              <select
                value={imageProvider}
                onChange={(e) => { setImageProvider(e.target.value); updateSetting("mcp_image_provider", e.target.value); }}
                className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
              >
                <option value="">— Select provider —</option>
                {providers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Model</label>
              <select
                value={imageModel}
                onChange={(e) => updateSetting("mcp_image_model", e.target.value)}
                className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
              >
                <option value="">— Select model —</option>
                {imageModels.map((m) => <option key={m.id} value={m.name}>{m.name}</option>)}
              </select>
            </div>
            {saving?.startsWith("mcp_image") && <div className="flex items-center gap-1 text-xs text-green-500"><Check className="w-3 h-3" /> Saved</div>}
          </div>
        </div>

        {/* Text-to-Speech */}
        <div className="p-4 rounded-xl border border-border bg-card">
          <div className="flex items-center gap-2 mb-3">
            <Volume2 className="w-4 h-4 text-primary" />
            <span className="text-sm font-semibold">Text-to-Speech</span>
          </div>
          <p className="text-xs text-muted-foreground mb-3">Tool: <code>text_to_speech</code> — text → audio</p>
          <div className="space-y-3">
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">Provider</label>
              <select
                value={ttsProvider}
                onChange={(e) => { setTtsProvider(e.target.value); updateSetting("mcp_tts_provider", e.target.value); }}
                className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
              >
                <option value="">— Select provider —</option>
                {providers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Model</label>
                <select
                  value={ttsModel}
                  onChange={(e) => updateSetting("mcp_tts_model", e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
                >
                  <option value="">— Select model —</option>
                  {ttsModels.map((m) => <option key={m.id} value={m.name}>{m.name}</option>)}
                </select>
              </div>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Voice</label>
                <select
                  value={ttsVoice}
                  onChange={(e) => updateSetting("mcp_tts_voice", e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
                >
                  <option value="alloy">Alloy</option>
                  <option value="echo">Echo</option>
                  <option value="fable">Fable</option>
                  <option value="onyx">Onyx</option>
                  <option value="nova">Nova</option>
                  <option value="shimmer">Shimmer</option>
                </select>
              </div>
            </div>
            {saving?.startsWith("mcp_tts") && <div className="flex items-center gap-1 text-xs text-green-500"><Check className="w-3 h-3" /> Saved</div>}
          </div>
        </div>

        {/* Vision */}
        <div className="p-4 rounded-xl border border-border bg-card">
          <div className="flex items-center gap-2 mb-3">
            <Eye className="w-4 h-4 text-primary" />
            <span className="text-sm font-semibold">Vision (Analyze Image)</span>
          </div>
          <p className="text-xs text-muted-foreground mb-3">Tool: <code>analyze_image</code> — image → text</p>
          <p className="text-xs text-muted-foreground">
            Uses Vision tool settings. <Link href="/tools/vision" className="text-primary hover:underline">Configure →</Link>
          </p>
        </div>
      </div>
    </div>
  );
}
