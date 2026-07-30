import { cn } from "@/lib/utils";
import { getProviderInitials, getProviderLogo } from "@/lib/provider-logos";
import type { Provider } from "@/lib/api";
import Image from "next/image";

type NeonColor = "cyan" | "magenta" | "green" | "amber" | "purple";

const neonCycle: NeonColor[] = ["cyan", "magenta", "green", "amber", "purple"];

export function providerNeonColor(name: string): NeonColor {
  const sum = name.split("").reduce((acc, ch) => acc + ch.charCodeAt(0), 0);
  return neonCycle[sum % neonCycle.length] ?? "cyan";
}

const neonStyles: Record<NeonColor, { bg: string; text: string; border: string }> = {
  cyan: { bg: "bg-neon-cyan/10", text: "text-neon-cyan", border: "border-neon-cyan/20" },
  magenta: { bg: "bg-neon-magenta/10", text: "text-neon-magenta", border: "border-neon-magenta/20" },
  green: { bg: "bg-neon-green/10", text: "text-neon-green", border: "border-neon-green/20" },
  amber: { bg: "bg-neon-amber/10", text: "text-neon-amber", border: "border-neon-amber/20" },
  purple: { bg: "bg-neon-purple/10", text: "text-neon-purple", border: "border-neon-purple/20" },
};

export const NEON_BORDER_HOVER: Record<NeonColor, string> = {
  cyan: "hover:border-neon-cyan/40",
  magenta: "hover:border-neon-magenta/40",
  green: "hover:border-neon-green/40",
  amber: "hover:border-neon-amber/40",
  purple: "hover:border-neon-purple/40",
};

export const NEON_ACCENT_STRIP: Record<NeonColor, string> = {
  cyan: "before:bg-neon-cyan",
  magenta: "before:bg-neon-magenta",
  green: "before:bg-neon-green",
  amber: "before:bg-neon-amber",
  purple: "before:bg-neon-purple",
};

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
  const neon = providerNeonColor(provider.name);
  const styles = neonStyles[neon];

  const sizeClasses = {
    sm: "w-8 h-8 text-sm rounded-lg",
    md: "w-10 h-10 text-base rounded-[10px]",
    lg: "w-12 h-12 text-[22px] rounded-xl",
  };

  return (
    <div
      className={cn(
        "shrink-0 flex items-center justify-center font-heading font-bold border",
        styles.bg,
        styles.text,
        styles.border,
        sizeClasses[size],
        className
      )}
      aria-hidden={logo ? "true" : undefined}
    >
      {logo ? (
        <Image
          src={logo}
          alt=""
          width={size === "lg" ? 32 : 24}
          height={size === "lg" ? 32 : 24}
          className="object-contain"
        />
      ) : (
        initials
      )}
    </div>
  );
}

export function AuthTypeBadge({ authType }: { authType: Provider["auth_type"] }) {
  const isKey = authType === "apikey";
  return (
    <span className="text-[11px] text-muted-foreground font-medium">
      {isKey ? "API Key" : "OAuth"}
    </span>
  );
}

export function ProviderTypeBadge({
  providerType,
}: {
  providerType: Provider["provider_type"];
}) {
  if (providerType === "custom") return null;
  return (
    <>
      <span className="text-[11px] text-muted-foreground">·</span>
      <span className="text-[11px] text-muted-foreground font-medium">
        Built-in
      </span>
    </>
  );
}

export function StatusPill({
  status,
  label,
}: {
  status: Provider["status"];
  label?: string;
}) {
  const isOnline = status === "online";
  return (
    <span className="inline-flex items-center gap-1 text-[11px] font-medium">
      <span
        className={cn(
          "w-1.5 h-1.5 rounded-full",
          isOnline ? "bg-green-500" : "bg-red-400"
        )}
      />
      <span className={isOnline ? "text-green-600" : "text-red-400"}>
        {label ?? (isOnline ? "Online" : "Offline")}
      </span>
    </span>
  );
}

export function maskKey(value: string): string {
  if (!value || value.length <= 8) return value;
  const visible = value.slice(-4);
  const prefix = value.slice(0, Math.min(4, value.indexOf("-") + 1 || 4));
  return `${prefix}${String.fromCharCode(8226).repeat(8)}${visible}`;
}
