// Provider logo mapping: maps builtin_id or name to actual asset path
const PROVIDER_LOGOS: Record<string, string> = {
  xiaomi: "/assets/xiaomi-mimo.png",
  meta: "/assets/meta.svg",
  google: "/assets/logo_ai_studio_color_1x_web_512dp.png",
  kimchi: "/assets/kimchi.png",
  openrouter: "/assets/openrouter.png",
  deepseek: "/assets/deepseek.ico",
  cloudflare: "/assets/cloudflare.svg",
  grok: "/assets/grok.png",
  ollama: "/assets/ollama.avif",
  ollamacloud: "/assets/ollama.avif",
  anigravity: "/assets/antigravtiy.ico",
  "fatherless-ai": "/assets/fatherlesai.svg",
  fatherles: "/assets/fatherles-ai.ico",
  runapi: "/assets/runapi.svg",
  stepfun: "/assets/stepfun.svg",
  hcnsec: "/assets/hcnsec.png",
  "openai-codex": "/assets/openai.svg",
};

export function getProviderLogo(provider: { builtin_id?: string | null; name?: string; icon?: string }): string | null {
  // Try builtin_id first
  if (provider.builtin_id) {
    const key = provider.builtin_id.toLowerCase();
    if (PROVIDER_LOGOS[key]) return PROVIDER_LOGOS[key];
    // Try partial match
    for (const [k, path] of Object.entries(PROVIDER_LOGOS)) {
      if (key.includes(k) || k.includes(key)) return path;
    }
  }
  // Try name match
  const nameLower = (provider.name ?? "").toLowerCase();
  for (const [key, path] of Object.entries(PROVIDER_LOGOS)) {
    if (nameLower.includes(key)) return path;
  }
  // Try icon field
  if (provider.icon) {
    for (const [key, path] of Object.entries(PROVIDER_LOGOS)) {
      if (provider.icon.toLowerCase().includes(key)) return path;
    }
    // If icon is a URL path (e.g. /api/providers/favicon?url=...), use it directly
    if (provider.icon.startsWith("/") || provider.icon.startsWith("http")) {
      return provider.icon;
    }
  }
  return null;
}

export function getProviderInitials(name: string): string {
  return name
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}
