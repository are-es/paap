export interface Provider {
  id: number;
  name: string;
  base_url: string;
  auth_type: "apikey" | "connection";
  provider_type: "builtin" | "custom";
  builtin_id?: string | null;
  status: "online" | "offline";
  is_active?: boolean;
  key_count?: number;
  active_key_count?: number;
  connection_count?: number;
  model_count?: number;
  round_robin?: boolean;
  round_robin_enabled?: boolean;
  icon?: string;
  supports_anthropic?: boolean;
}

export interface ApiKeyItem {
  id: number;
  provider_id: number;
  name: string;
  key: string;
  key_masked?: string;
  is_active: boolean;
  last_used?: string | null;
  fail_count?: number;
}

export interface ConnectionItem {
  id: number;
  provider_id: number;
  name: string;
  email?: string | null;
  is_active: boolean;
  test_status?: string | null;
}

export interface ModelItem {
  id: string;
  name: string;
  selected?: boolean;
}

export interface PlaygroundResult {
  status: number;
  latency_ms: number;
  tokens_in?: number;
  tokens_out?: number;
  res: string;
  key?: string;
}

export interface SettingsData {
  base_url?: string;
  gateway_key?: string;
  proxy_enabled?: boolean;
  prompt_injection_enabled?: boolean;
  prompt_injection_text?: string;
  prompt_injection_position?: "prepend" | "append";
  compression_mode?: string;
  compression_level?: string;
  headroom_enabled?: string;
  headroom_url?: string;
  headroom_timeout_ms?: string;
  [key: string]: unknown;
}

export interface HeadroomStatus {
  enabled: boolean;
  url: string;
  reachable: boolean;
  installed: boolean;
  /** Present only when the stage cannot run — what is wrong. */
  hint?: string;
  /** Present only when the stage cannot run — the command to fix it. */
  command?: string;
}

export interface CompressionSkill {
  id: string;
  name: string;
  description?: string;
  intensity: "off" | "lite" | "full" | "ultra";
  categories?: string[];
  custom_rules?: string[];
  position: number;
  is_active: boolean;
  is_builtin: boolean;
}

export interface LogEntry {
  id: number;
  timestamp: string;
  provider_name?: string;
  key_name?: string;
  model_id?: string;
  group_name?: string;
  proxy_name?: string;
  proxy_used?: string;
  status_code: number;
  tokens_in?: number;
  tokens_out?: number;
  latency_ms?: number;
  cost_usd?: number;
  compression_ratio?: number;
  error?: string;
}

export interface CostSummary {
  summary: {
    today: { cost_usd: number; req_count: number; tokens_in: number; tokens_out: number };
    "7d": { cost_usd: number; req_count: number; tokens_in: number; tokens_out: number };
    "30d": { cost_usd: number; req_count: number; tokens_in: number; tokens_out: number };
    all: { cost_usd: number; req_count: number; tokens_in: number; tokens_out: number };
  };
  by_provider?: { provider_name: string; cost_usd: number; req_count: number }[];
  by_model?: { model_id: string; provider_name: string; cost_usd: number; req_count: number }[];
}

export interface GroupItem {
  id: string;
  name: string;
  icon?: string;
  race_mode: "race_all" | "race_keys" | "round_robin" | "fail_first" | "rr_race_keys";
  selected_keys?: string[];
  selected_models?: string[];
  race_count?: number;
  max_keys?: number;
  round_robin?: boolean;
  parallel?: number;
  created_at?: string;
}

export interface GroupModel {
  id: string;
  group_id: string;
  provider_id: string;
  model_id: string;
  provider_name?: string;
  provider_icon?: string;
}

export interface ProxyItem {
  id: number;
  address: string;
  port: number;
  type: string;
  status: "active" | "inactive" | "testing";
  latency_ms?: number;
  country?: string;
  last_tested?: string;
}

export interface GatewayKey {
  id: number;
  name: string;
  key: string;
  key_masked: string;
  is_active: boolean;
  created_at: string;
}

async function fetchApi<T>(input: string, init?: RequestInit): Promise<T> {
  const res = await fetch(input, init);
  if (!res.ok) {
    const text = await res.text().catch(() => "Unknown error");
    throw new Error(`${res.status} ${res.statusText}: ${text}`);
  }
  return res.json() as Promise<T>;
}

export const api = {
  // --- Settings ---
  getSettings: () => fetchApi<SettingsData>("/api/settings"),
  settings: () => fetchApi<SettingsData>("/api/settings"),
  updateSettings: (payload: Partial<SettingsData>) =>
    fetchApi<SettingsData>("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),
  headroomStatus: () => fetchApi<HeadroomStatus>("/api/headroom/status"),

  // --- Providers ---
  getProviders: () => fetchApi<Provider[]>("/api/providers"),
  providers: () => fetchApi<Provider[]>("/api/providers"),

  addProvider: (payload: {
    name: string;
    base_url: string;
    auth_type: "apikey" | "connection";
    supports_anthropic?: boolean;
  }) =>
    fetchApi<Provider>("/api/providers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...payload, provider_type: "custom" }),
    }),

  deleteProvider: (id: number | string) =>
    fetchApi<{ status: string; id: string }>(`/api/providers/${id}`, { method: "DELETE" }),

  getProvider: (id: number | string) => fetchApi<Provider>(`/api/providers/${id}`),

  toggleRoundRobin: (id: number | string, enabled: boolean) =>
    fetchApi<Provider>(`/api/providers/${id}/round-robin`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled }),
    }),

  // --- Keys ---
  getKeys: (providerId: number | string) =>
    fetchApi<ApiKeyItem[]>(`/api/providers/${providerId}/keys`),

  addKey: (providerId: number | string, payload: { name?: string; key: string }) =>
    fetchApi<ApiKeyItem>(`/api/providers/${providerId}/keys`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  bulkAddKeys: (providerId: number | string, keys: string[]) =>
    fetchApi<ApiKeyItem[]>(`/api/providers/${providerId}/keys`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ keys }),
    }),

  bulkAddKeysWithNames: (providerId: number | string, items: { key: string; name: string }[]) =>
    fetchApi<ApiKeyItem[]>(`/api/providers/${providerId}/keys`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ keys: items.map(i => i.key), names: items.map(i => i.name) }),
    }),

  deleteKey: (providerId: number | string, keyId: number | string) =>
    fetchApi<{ status: string; id: string }>(`/api/providers/${providerId}/keys/${keyId}`, { method: "DELETE" }),

  deleteDisabledKeys: (providerId: number | string) =>
    fetchApi<{ status: string; deleted: number }>(`/api/providers/${providerId}/keys/disabled`, {
      method: "POST",
    }),

  enableAllKeys: (providerId: number | string) =>
    fetchApi<{ enabled: number }>(`/api/providers/${providerId}/keys/enable-all`, {
      method: "POST",
    }),

  toggleKey: (providerId: number | string, keyId: number) =>
    fetchApi<ApiKeyItem>(
      `/api/providers/${providerId}/keys/${keyId}`,
      { method: "PATCH" }
    ),

  testKey: (providerId: number | string, keyId: number) =>
    fetchApi<{ status: string; latency_ms: number }>(
      `/api/providers/${providerId}/keys/${keyId}/test`,
      { method: "POST" }
    ),

  // --- Connections ---
  getConnections: (providerId: number | string) =>
    fetchApi<ConnectionItem[]>(`/api/providers/${providerId}/connections`),

  addConnection: (providerId: number | string) =>
    fetchApi<ConnectionItem>(`/api/providers/${providerId}/connections`, { method: "POST" }),

  deleteConnection: (providerId: number | string, connId: number | string) =>
    fetchApi<{ status: string; id: string }>(`/api/providers/${providerId}/connections/${connId}`, {
      method: "DELETE",
    }),

  // --- Models ---
  getModels: (providerId: number | string) =>
    fetchApi<ModelItem[]>(`/api/providers/${providerId}/models`),

  detectModels: (providerId: number | string) =>
    fetchApi<ModelItem[]>(`/api/providers/${providerId}/models/detect`, {
      method: "POST",
    }),

  updateModels: (providerId: number | string, modelIds: string[]) =>
    fetchApi<ModelItem[]>(`/api/providers/${providerId}/models`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ models: modelIds }),
    }),

  // --- Playground ---
  runPlayground: (
    providerId: number | string,
    payload: {
      key_id?: string | number | "all";
      model_id: string;
      prompt: string;
    }
  ) =>
    fetchApi<PlaygroundResult>(`/api/providers/${providerId}/playground`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  // --- Compression Skills (removed) ---

  getCompressionLogs: (params?: { limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    return fetchApi<LogEntry[]>(`/api/compression/logs?${qs}`);
  },

  // --- Logs ---
  getLogs: (params?: {
    provider?: string;
    model?: string;
    status?: string;
    search?: string;
    limit?: number;
    offset?: number;
  }) => {
    const qs = new URLSearchParams();
    if (params?.provider) qs.set("provider", params.provider);
    if (params?.model) qs.set("model", params.model);
    if (params?.status) qs.set("status", params.status);
    if (params?.search) qs.set("search", params.search);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    return fetchApi<{ data: LogEntry[]; total: number; page: number; per_page: number }>(`/api/logs?${qs}`).then(r => r.data);
  },

  clearLogs: () =>
    fetchApi<{ status: string; message: string }>("/api/logs", { method: "DELETE" }),

  getCost: () => fetchApi<CostSummary>("/api/logs/cost"),

  exportLogs: (format: "csv" | "json") =>
    `/api/logs/export?format=${format}`,

  // --- Groups ---
  getGroups: () => fetchApi<GroupItem[]>("/api/groups"),

  getGroup: (id: string) => fetchApi<GroupItem>(`/api/groups/${id}`),

  getGroupModels: (groupId: string) =>
    fetchApi<GroupModel[]>(`/api/groups/${groupId}/models`),

  addGroup: (payload: {
    name: string;
    race_mode?: string;
    selected_keys?: string[];
    selected_models?: string[];
    race_count?: number;
    max_keys?: number;
  }) =>
    fetchApi<GroupItem>("/api/groups", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  updateGroup: (id: string, payload: Partial<GroupItem>) =>
    fetchApi<GroupItem>(`/api/groups/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  deleteGroup: (id: string) =>
    fetchApi<{ status: string; id: string }>(`/api/groups/${id}`, { method: "DELETE" }),

  addGroupModel: (groupId: string, payload: { provider_id: string; model_id: string }) =>
    fetchApi<GroupModel>(`/api/groups/${groupId}/models`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  removeGroupModel: (groupId: string, modelId: string) =>
    fetchApi<{ status: string }>(`/api/groups/${groupId}/models/${encodeURIComponent(modelId)}`, { method: "DELETE" }),

  // --- Proxies ---
  getProxies: () => fetchApi<ProxyItem[]>("/api/proxies"),

  addProxy: (payload: {
    address: string;
    port: number;
    type: string;
    username?: string;
    password?: string;
  }) =>
    fetchApi<ProxyItem>("/api/proxies", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  deleteProxy: (id: number | string) =>
    fetchApi<{ status: string; id: string }>(`/api/proxies/${id}`, { method: "DELETE" }),

  testProxy: (id: number) =>
    fetchApi<{ status: string; latency_ms: number }>(
      `/api/proxies/${id}/test`,
      { method: "POST" }
    ),

  testAllProxies: () =>
    fetchApi<{ results: { id: number; status: string; latency_ms: number }[] }>(
      "/api/proxies/test-all",
      { method: "POST" }
    ),

  // --- Gateway Keys ---
  getGatewayKeys: () => fetchApi<GatewayKey[]>("/api/gateway/keys"),

  addGatewayKey: (payload: { name: string }) =>
    fetchApi<GatewayKey>("/api/gateway/keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }),

  deleteGatewayKey: (id: number | string) =>
    fetchApi<{ status: string; id: string }>(`/api/gateway/keys/${id}`, { method: "DELETE" }),

  // --- System ---
  // shutdown/restart intentionally use raw fetch: the server kills itself right
  // after responding, so the connection often dies before the body arrives.
  // fetchApi would throw on that and mask a successful shutdown.
  shutdown: () =>
    fetch("/api/system/shutdown", { method: "POST" }),

  restart: () =>
    fetch("/api/system/restart", { method: "POST" }),
};
