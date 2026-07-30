"use client";

import { useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type GroupItem, type GroupModel } from "@/lib/api";
import {
  ChevronRight,
  ChevronDown,
  ChevronUp,
  RotateCw,
  Zap,
  Plus,
  Trash2,
  X,
  Cpu,
  Loader2,
  Key,
  Settings2,
  Layers,
} from "lucide-react";
import { cn } from "@/lib/utils";

const MODE_OPTIONS = [
  { value: "rr_race_keys", label: "RR + Race Keys", icon: Zap, desc: "Rotate models, race keys per model — best of both" },
  { value: "race_all", label: "Race All", icon: Zap, desc: "Cross-provider race, fastest wins" },
  { value: "race_keys", label: "Race Keys", icon: Key, desc: "Race N keys within provider" },
  { value: "round_robin", label: "Round Robin", icon: RotateCw, desc: "Rotate models per request" },
  { value: "fail_first", label: "Fail First", icon: ChevronDown, desc: "Cascade fallback A→B→C" },
] as const;

export function GroupSetupClient() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();

  const groupQuery = useQuery({
    queryKey: ["group", id],
    queryFn: () => api.getGroup(id),
    enabled: id !== "new",
  });

  const group = groupQuery.data;

  if (id === "new") {
    return <NewGroupForm />;
  }

  if (groupQuery.isLoading) {
    return (
      <div className="p-6 md:p-8 min-h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!group) {
    return (
      <div className="p-6 md:p-8 min-h-full">
        <p className="text-muted-foreground">Group not found.</p>
        <Link href="/groups" className="text-primary text-sm hover:underline mt-2 inline-block">
          ← Back to Groups
        </Link>
      </div>
    );
  }

  return (
    <div className="p-6 md:p-8 min-h-full">
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/groups" className="hover:text-foreground transition-colors">
          Groups
        </Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">{group.name}</span>
      </nav>

      <GroupHeader group={group} />

      <ModelsSection groupId={group.id} />

      <RaceConditionSection group={group} />

      <DangerZoneSection groupId={group.id} groupName={group.name} />
    </div>
  );
}

function GroupHeader({ group }: { group: GroupItem }) {
  const queryClient = useQueryClient();
  const isRR = group.race_mode === "round_robin" || group.race_mode === "rr_race_keys";

  const toggleRR = () => {
    const newMode =
      isRR
        ? group.race_mode === "rr_race_keys"
          ? "race_keys"
          : "fail_first"
        : group.race_mode === "race_keys"
          ? "rr_race_keys"
          : "round_robin";
    api
      .updateGroup(group.id, { race_mode: newMode as GroupItem["race_mode"] })
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ["group", group.id] });
        queryClient.invalidateQueries({ queryKey: ["groups"] });
      });
  };

  return (
    <div className="flex items-center justify-between gap-4 mb-8">
      <h1 className="font-heading text-xl font-bold">{group.name}</h1>
      <div className="flex items-center gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <RotateCw className="w-4 h-4 text-muted-foreground" />
          <span className="text-sm font-medium">RR</span>
        </div>
        <button
          onClick={toggleRR}
          className={cn(
            "w-9 h-5 rounded-full relative transition-colors shrink-0",
            isRR ? "bg-primary" : "bg-muted"
          )}
        >
          <span
            className={cn(
              "absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform",
              isRR ? "left-[18px]" : "left-0.5"
            )}
          />
        </button>
      </div>
    </div>
  );
}

function Section({
  title,
  children,
  defaultOpen = false,
  rightSlot,
}: {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
  rightSlot?: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="mb-6 border border-border rounded-xl bg-card overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between px-4 py-3 hover:bg-secondary/50 transition-colors"
      >
        <span className="font-medium text-sm">{title}</span>
        <div className="flex items-center gap-2">
          {rightSlot}
          {open ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
        </div>
      </button>
      {open && <div className="px-4 pb-4 border-t border-border">{children}</div>}
    </div>
  );
}

function AddModelModal({
  open,
  onClose,
  groupId,
}: {
  open: boolean;
  onClose: () => void;
  groupId: string;
}) {
  const queryClient = useQueryClient();
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => api.getProviders(),
    enabled: open,
  });

  const modelsQuery = useQuery({
    queryKey: ["models", provider],
    queryFn: () => api.getModels(provider),
    enabled: !!provider && open,
  });

  const addMutation = useMutation({
    mutationFn: () => api.addGroupModel(groupId, { provider_id: provider, model_id: model }),
    onSuccess: (newModel) => {
      // Update cache directly without refetch (avoids re-render that might close modal)
      queryClient.setQueryData(["group-models", groupId], (old: GroupModel[] | undefined) => {
        return old ? [...old, newModel] : [newModel];
      });
      setModel("");
    },
  });

  if (!open) return null;

  const providers = providersQuery.data ?? [];
  const models = (modelsQuery.data ?? []).filter((m: { selected?: boolean }) => m.selected);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-sm bg-card rounded-xl border border-border p-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-heading text-lg font-bold">Add Model</h2>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-muted text-muted-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-xs font-medium mb-1.5 text-muted-foreground">Provider</label>
            <select
              value={provider}
              onChange={(e) => { setProvider(e.target.value); setModel(""); }}
              className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm focus:outline-none focus:ring-1 focus:ring-ring"
            >
              <option value="">Select provider...</option>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs font-medium mb-1.5 text-muted-foreground">Model</label>
            <select
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              disabled={!provider}
            >
              <option value="">Select model...</option>
              {models.map((m: any) => (
                <option key={m.id} value={m.name}>{m.name}</option>
              ))}
            </select>
          </div>

          <div className="flex justify-end gap-2">
            <button
              onClick={onClose}
              className="px-3 py-1.5 text-sm rounded-lg border border-border hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={() => { if (provider && model) addMutation.mutate(); }}
              disabled={!provider || !model || addMutation.isPending}
              className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground disabled:opacity-50"
            >
              {addMutation.isPending ? "Adding..." : "Add"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function ModelsSection({ groupId }: { groupId: string }) {
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);

  const modelsQuery = useQuery({
    queryKey: ["group-models", groupId],
    queryFn: () => api.getGroupModels(groupId),
  });

  const removeMutation = useMutation({
    mutationFn: (modelId: string) => api.removeGroupModel(groupId, modelId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["group-models", groupId] }),
  });

  const models = modelsQuery.data ?? [];

  return (
    <>
      <Section
        title="Models"
        defaultOpen={true}
        rightSlot={
          <button
            onClick={(e: any) => { e.stopPropagation(); setModalOpen(true); }}
            className="inline-flex items-center gap-1 px-2.5 py-1 text-xs rounded-lg bg-primary text-primary-foreground shrink-0"
          >
            <Plus className="w-3 h-3" />
            Add Model
          </button>
        }
      >
        <div className="flex flex-wrap gap-2 mt-3">
          {models.length === 0 && (
            <p className="text-xs text-muted-foreground py-2 w-full">No models. Click + Add Model to add one.</p>
          )}
          {models.map((m: GroupModel) => (
            <span
              key={m.id}
              className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border border-border bg-secondary text-xs font-mono text-secondary-foreground"
            >
              {m.model_id}
              <button
                onClick={() => removeMutation.mutate(m.model_id)}
                className="ml-0.5 p-0.5 rounded-full hover:bg-destructive/10 text-muted-foreground hover:text-destructive"
              >
                <X className="w-3 h-3" />
              </button>
            </span>
          ))}
        </div>
      </Section>

      <AddModelModal open={modalOpen} onClose={() => setModalOpen(false)} groupId={groupId} />
    </>
  );
}

function RaceConditionSection({ group }: { group: GroupItem }) {
  const queryClient = useQueryClient();
  const [raceCount, setRaceCount] = useState(group.max_keys ?? group.race_count ?? 1);
  const [raceEnabled, setRaceEnabled] = useState(
    group.race_mode === "race_keys" || group.race_mode === "rr_race_keys"
  );

  const updateMutation = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.updateGroup(group.id, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["group", group.id] });
      queryClient.invalidateQueries({ queryKey: ["groups"] });
    },
  });

  const toggleRace = () => {
    const newEnabled = !raceEnabled;
    setRaceEnabled(newEnabled);
    updateMutation.mutate({ race_mode: newEnabled ? "rr_race_keys" : "round_robin" });
  };

  const updateCount = (val: number) => {
    setRaceCount(val);
    updateMutation.mutate({ max_keys: val, race_count: val });
  };

  return (
    <div className="mb-4 border border-border rounded-lg bg-card overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-3">
          <span className="font-medium text-sm">Race Condition</span>
          <select
            value={raceCount}
            onChange={(e) => updateCount(Number(e.target.value))}
            className="px-2 py-1 rounded border border-input bg-background text-xs"
          >
            {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
        </div>
        <button
          onClick={toggleRace}
          className={cn(
            "w-9 h-5 rounded-full relative transition-colors",
            raceEnabled ? "bg-green-500" : "bg-gray-300 dark:bg-gray-600"
          )}
        >
          <span className={cn(
            "absolute top-[2px] w-4 h-4 rounded-full shadow-sm transition-all bg-white",
            raceEnabled ? "left-[18px]" : "left-[2px]"
          )} />
        </button>
      </div>
    </div>
  );
}

function NewGroupForm() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [mode, setMode] = useState("rr_race_keys");

  const createMutation = useMutation({
    mutationFn: () => api.addGroup({ name: name.trim(), race_mode: mode, max_keys: 1 }),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["groups"] });
      router.push(`/groups/${data.id}`);
    },
  });

  return (
    <div className="p-6 md:p-8 min-h-full max-w-xl">
      <nav className="flex items-center gap-1.5 text-sm text-muted-foreground mb-6">
        <Link href="/groups" className="hover:text-foreground transition-colors">Groups</Link>
        <ChevronRight className="w-3.5 h-3.5" />
        <span className="text-foreground font-medium">New Group</span>
      </nav>

      <h1 className="font-heading text-xl font-bold mb-6">Create Group</h1>

      <div className="space-y-4">
        <div>
          <label className="block text-sm font-medium mb-1.5">Name</label>
          <input
            placeholder="e.g. fast, cheap, coding"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full px-3 py-2 rounded-lg border border-input bg-background text-sm"
            autoFocus
          />
        </div>

        <div>
          <label className="block text-sm font-medium mb-1.5">Routing Mode</label>
          <div className="grid grid-cols-1 gap-2">
            {MODE_OPTIONS.map(({ value, label, icon: Icon, desc }) => (
              <button
                key={value}
                onClick={() => setMode(value)}
                className={cn(
                  "p-3 rounded-lg border text-left transition-colors",
                  mode === value ? "border-primary bg-primary/5" : "border-border hover:bg-secondary"
                )}
              >
                <div className="flex items-center gap-2 mb-0.5">
                  <Icon className="w-4 h-4" />
                  <span className="text-sm font-medium">{label}</span>
                </div>
                <p className="text-xs text-muted-foreground">{desc}</p>
              </button>
            ))}
          </div>
        </div>

        <button
          onClick={() => { if (name.trim()) createMutation.mutate(); }}
          disabled={!name.trim() || createMutation.isPending}
          className="w-full px-4 py-2.5 text-sm rounded-lg bg-primary text-primary-foreground disabled:opacity-50"
        >
          {createMutation.isPending ? "Creating..." : "Create Group"}
        </button>
      </div>
    </div>
  );
}

function DangerZoneSection({ groupId, groupName }: { groupId: string; groupName: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteGroup(groupId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["groups"] });
      router.push("/groups");
    },
  });

  return (
    <Section title="Danger Zone">
      <div className="mt-2 flex items-center justify-between gap-4">
        <div className="min-w-0">
          <p className="text-sm font-medium">Delete this group</p>
          <p className="text-xs text-muted-foreground">This cannot be undone. All models will be removed from this group.</p>
        </div>
        <button
          onClick={() => { if (confirm(`Delete group "${groupName}"?`)) deleteMutation.mutate(); }}
          disabled={deleteMutation.isPending}
          className="px-4 py-2 text-sm rounded-lg bg-destructive text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50 shrink-0"
        >
          {deleteMutation.isPending ? "Deleting..." : "Delete Group"}
        </button>
      </div>
    </Section>
  );
}
