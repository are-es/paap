"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Eye, Globe, Power, PowerOff, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

export default function ToolsPage() {
  const toolsQuery = useQuery({
    queryKey: ["tools"],
    queryFn: () => api.getTools(),
  });

  const mcpStatusQuery = useQuery({
    queryKey: ["mcp-status"],
    queryFn: () => fetch("/mcp/status").then((r) => r.json()),
    refetchInterval: 30_000,
  });

  const tools: any[] = toolsQuery.data || [];
  const visionTool = tools.find((t) => t.type === "vision");
  const mcpStatus = mcpStatusQuery.data;

  return (
    <div className="p-6 md:p-8 min-h-full">
      <h1 className="font-heading text-2xl font-bold mb-7">Tools</h1>

      {toolsQuery.isError ? (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">
          Failed to load tools: {(toolsQuery.error as Error)?.message || "Unknown error"}
        </div>
      ) : toolsQuery.isLoading ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="h-32 rounded-xl border border-border bg-muted/30 animate-pulse"
            />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {/* Vision Card */}
          <Link
            href="/tools/vision"
            className={cn(
              "group relative flex flex-col gap-3 p-4 rounded-xl border border-border bg-card",
              "cursor-pointer transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/40"
            )}
          >
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-muted/50 flex items-center justify-center shrink-0">
                <Eye className="w-5 h-5 text-primary" />
              </div>
              <div className="flex-1 min-w-0">
                <h2 className="font-medium text-sm">Vision</h2>
              </div>
              <div className={cn(
                "w-2 h-2 rounded-full shrink-0",
                visionTool?.enabled ? "bg-green-500" : "bg-muted-foreground/30"
              )} />
            </div>
          </Link>

          {/* MCP Tools Card */}
          <Link
            href="/tools/mcp"
            className={cn(
              "group relative flex flex-col gap-3 p-4 rounded-xl border border-border bg-card",
              "cursor-pointer transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/40"
            )}
          >
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-muted/50 flex items-center justify-center shrink-0">
                <Globe className="w-5 h-5 text-primary" />
              </div>
              <div className="flex-1 min-w-0">
                <h2 className="font-medium text-sm">MCP Tools</h2>
                <p className="text-[10px] text-muted-foreground mt-0.5">
                  {mcpStatus?.enabled
                    ? `${mcpStatus.tools ?? 3} tools active`
                    : "Image gen, TTS, Vision via MCP"}
                </p>
              </div>
              <div className={cn(
                "w-2 h-2 rounded-full shrink-0",
                mcpStatus?.enabled ? "bg-green-500" : "bg-muted-foreground/30"
              )} />
            </div>
          </Link>
        </div>
      )}
    </div>
  );
}
