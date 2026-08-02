"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Eye, Power, PowerOff, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

export default function ToolsPage() {
  const toolsQuery = useQuery({
    queryKey: ["tools"],
    queryFn: () => api.getTools(),
  });

  const tools: any[] = toolsQuery.data || [];
  const visionTool = tools.find((t) => t.type === "vision");

  return (
    <div className="p-6 md:p-8 min-h-full">
      <h1 className="font-heading text-2xl font-bold mb-7">Tools</h1>

      {toolsQuery.isLoading ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {Array.from({ length: 2 }).map((_, i) => (
            <div
              key={i}
              className="h-32 rounded-xl border border-primary/15 bg-primary/[0.04] animate-pulse"
            />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {/* Vision Card */}
          <Link
            href="/tools/vision"
            className={cn(
              "group relative flex flex-col gap-3 p-4 rounded-lg border border-primary/15 bg-primary/[0.04]",
              "cursor-pointer transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/40"
            )}
          >
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-muted/50 flex items-center justify-center shrink-0">
                <Eye className="w-5 h-5 text-primary" />
              </div>
              <div className="flex-1 min-w-0">
                <h2 className="font-medium text-sm">Vision</h2>
                <p className="text-[10px] text-muted-foreground mt-0.5">
                  {visionTool?.enabled
                    ? `→ ${visionTool.route_model}`
                    : "Auto-route gambar ke model vision"}
                </p>
              </div>
              <div className={cn(
                "w-2 h-2 rounded-full shrink-0",
                visionTool?.enabled ? "bg-green-500" : "bg-muted-foreground/30"
              )} />
            </div>
          </Link>

          {/* Placeholder for future tools */}
          <div className="flex flex-col gap-3 p-4 rounded-lg border border-dashed border-border bg-secondary/30 items-center justify-center min-h-[88px] opacity-50">
            <p className="text-xs text-muted-foreground">Coming soon...</p>
          </div>
        </div>
      )}
    </div>
  );
}
