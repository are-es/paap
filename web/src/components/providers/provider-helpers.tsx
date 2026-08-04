import { getProviderInitials, getProviderLogo } from "@/lib/provider-logos";
import type { Provider } from "@/lib/api";
import Image from "next/image";

const COLORS = ["#22d3ee", "#f472b6", "#34d399", "#fbbf24", "#a78bfa"] as const;

export function providerNeonColor(name: string): string {
  const sum = name.split("").reduce((acc, ch) => acc + ch.charCodeAt(0), 0);
  return COLORS[sum % COLORS.length] ?? COLORS[0];
}

// Tailwind classes for neon colors (used by provider cards)
const NEON_MAP: Record<string, { hover: string; strip: string }> = {
  "#22d3ee": { hover: "hover:border-neon-cyan/40", strip: "before:bg-neon-cyan" },
  "#f472b6": { hover: "hover:border-neon-magenta/40", strip: "before:bg-neon-magenta" },
  "#34d399": { hover: "hover:border-neon-green/40", strip: "before:bg-neon-green" },
  "#fbbf24": { hover: "hover:border-neon-amber/40", strip: "before:bg-neon-amber" },
  "#a78bfa": { hover: "hover:border-neon-purple/40", strip: "before:bg-neon-purple" },
};

export function getNeonClasses(color: string) {
  return NEON_MAP[color] ?? NEON_MAP[COLORS[0]];
}

export function ProviderIcon({
  provider,
  size = "md",
  className,
}: {
  provider: Pick<Provider, "name" | "builtin_id" | "icon">;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const logo = getProviderLogo(provider);
  const initials = getProviderInitials(provider.name);
  const color = providerNeonColor(provider.name);

  const sizeMap = {
    sm: "w-10 h-10 text-sm rounded-lg",
    md: "w-12 h-12 text-lg rounded-[10px]",       // 48px
    lg: "w-16 h-16 text-2xl rounded-xl",           // 64px
  };

  return (
    <div
      className={`${sizeMap[size]} flex items-center justify-center font-extrabold shrink-0 bg-card border ${className ?? ""}`}
      style={{ borderColor: `${color}30` }}
    >
      {logo ? (
        <Image src={logo} alt="" width={size === "sm" ? 20 : size === "lg" ? 36 : 26} height={size === "sm" ? 20 : size === "lg" ? 36 : 26} className="rounded-sm object-contain" unoptimized />
      ) : (
        <span className="text-lg" style={{ color }}>{initials}</span>
      )}
    </div>
  );
}

export function AuthTypeBadge({ authType }: { authType: string }) {
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wider bg-muted text-muted-foreground border border-border">
      {authType === "connection" ? "OAuth" : "API Key"}
    </span>
  );
}

export function ProviderTypeBadge({ providerType }: { providerType: string }) {
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wider border ${
      providerType === "builtin"
        ? "bg-primary/10 text-primary border-primary/20"
        : "bg-muted text-muted-foreground border-border"
    }`}>
      {providerType === "builtin" ? "Built-in" : "Custom"}
    </span>
  );
}

export function StatusPill({ status }: { status: string }) {
  const isOnline = status === "online";
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wider ${
      isOnline
        ? "bg-green-100 text-green-700 border border-green-200"
        : "bg-muted text-muted-foreground border border-border"
    }`}>
      <span className={`w-1.5 h-1.5 rounded-full ${isOnline ? "bg-green-500 animate-pulse" : "bg-muted-foreground"}`} />
      {status}
    </span>
  );
}
