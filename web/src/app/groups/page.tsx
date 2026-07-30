"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type GroupItem, type GroupModel } from "@/lib/api";
import { Plus, Layers, X } from "lucide-react";
import { cn } from "@/lib/utils";

export default function GroupsPage() {
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);

  const groupsQuery = useQuery({
    queryKey: ["groups"],
    queryFn: () => api.getGroups(),
  });

  const groupModelsQueries = useGroupModelCounts(groupsQuery.data ?? []);

  const groups = groupsQuery.data ?? [];

  return (
    <div className="p-6 md:p-8 min-h-full">
      <div className="flex items-center justify-between mb-7">
        <h1 className="font-heading text-2xl font-bold">Groups</h1>
        <button
          onClick={() => setAddOpen(true)}
          className="inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-lg bg-primary text-primary-foreground transition-shadow hover:opacity-90"
        >
          <Plus className="w-4 h-4" />
          Add Group
        </button>
      </div>

      {groupsQuery.isLoading && (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div
              key={i}
              className="h-32 rounded-xl border border-border bg-card animate-pulse"
            />
          ))}
        </div>
      )}

      {!groupsQuery.isLoading && groups.length === 0 && (
        <div className="text-center py-16 text-muted-foreground">
          <Layers className="w-10 h-10 mx-auto mb-3 opacity-30" />
          <p className="text-sm">No groups yet. Create one to route across providers.</p>
        </div>
      )}

      {!groupsQuery.isLoading && groups.length > 0 && (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4">
          {groups.map((group) => (
            <GroupCard
              key={group.id}
              group={group}
              models={groupModelsQueries[group.id] ?? []}
              onDelete={() => {
                api.deleteGroup(String(group.id)).then(() =>
                  queryClient.invalidateQueries({ queryKey: ["groups"] })
                );
              }}
            />
          ))}
        </div>
      )}

      <AddGroupModal open={addOpen} onClose={() => setAddOpen(false)} />
    </div>
  );
}

function useGroupModelCounts(groups: GroupItem[]) {
  const results = useQuery({
    queryKey: ["group-models-counts"],
    queryFn: async () => {
      const map: Record<string, GroupModel[]> = {};
      await Promise.all(
        groups.map(async (g) => {
          try {
            map[g.id] = await api.getGroupModels(g.id);
          } catch {
            map[g.id] = [];
          }
        })
      );
      return map;
    },
    enabled: groups.length > 0,
    staleTime: 60_000,
  });

  return results.data ?? {};
}

function GroupCard({
  group,
  models,
  onDelete,
}: {
  group: GroupItem;
  models: GroupModel[];
  onDelete: () => void;
}) {
  return (
    <Link
      href={`/groups/${group.id}`}
      className={cn(
        "group relative flex flex-col gap-3 p-4 rounded-lg border border-border bg-card",
        "cursor-pointer transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/40"
      )}
    >
      <div className="flex items-center gap-3">
        <div className="w-10 h-10 rounded-lg bg-muted/50 flex items-center justify-center shrink-0 overflow-hidden">
          <img src="/assets/grup.png" alt="" className="w-8 h-8 object-contain" />
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="font-medium text-sm truncate">{group.name}</h2>
          <p className="text-[10px] text-muted-foreground mt-0.5">
            {models.length} models · Race {group.max_keys ?? group.race_count ?? 0}
          </p>
        </div>
        <button
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDelete();
          }}
          className="p-1 rounded text-muted-foreground opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive transition-opacity"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
    </Link>
  );
}

function AddGroupModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [name, setName] = useState("");
  const queryClient = useQueryClient();

  const addMutation = useMutation({
    mutationFn: (n: string) => api.addGroup({ name: n }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["groups"] });
      setName("");
      onClose();
    },
  });

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-md bg-card rounded-xl border border-border p-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-heading text-lg font-bold">Add Group</h2>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-muted text-muted-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="space-y-4">
          <div>
            <label className="block text-xs font-medium mb-1.5 text-muted-foreground">Group Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. fast, cheap, coding"
              className="w-full px-3 py-2 text-sm rounded-lg border border-input bg-background focus:outline-none focus:ring-1 focus:ring-ring"
              autoFocus
              onKeyDown={(e) => { if (e.key === "Enter" && name.trim()) addMutation.mutate(name.trim()); }}
            />
          </div>
          <div className="flex justify-end gap-2">
            <button
              onClick={onClose}
              className="px-3 py-1.5 text-sm rounded-lg border border-border hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={() => { if (name.trim()) addMutation.mutate(name.trim()); }}
              disabled={!name.trim() || addMutation.isPending}
              className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground disabled:opacity-50 transition-shadow"
            >
              {addMutation.isPending ? "Creating..." : "Create"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
