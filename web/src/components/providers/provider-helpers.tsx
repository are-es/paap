import { useState } from "react";
import { getProviderInitials, getProviderLogo } from "@/lib/provider-logos";
import type { Provider } from "@/lib/api";
import Image from "next/image";

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
  const [imgError, setImgError] = useState(false);
  const showLogo = logo && !imgError;

  const sizeMap = {
    sm: "w-10 h-10 text-sm rounded-lg",
    md: "w-12 h-12 text-lg rounded-[10px]",
    lg: "w-16 h-16 text-2xl rounded-xl",
  };

  return (
    <div
      className={`${sizeMap[size]} flex items-center justify-center font-extrabold shrink-0 border border-border bg-muted/40 ${className ?? ""}`}
    >
      {showLogo ? (
        <Image src={logo} alt="" width={size === "sm" ? 20 : size === "lg" ? 36 : 26} height={size === "sm" ? 20 : size === "lg" ? 36 : 26} className="rounded-sm object-contain" unoptimized onError={() => setImgError(true)} />
      ) : (
        <span className="text-sm font-semibold text-muted-foreground">{initials}</span>
      )}
    </div>
  );
}

export function AuthTypeBadge({ authType }: { authType: string }) {
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-muted text-muted-foreground border border-border">
      {authType === "connection" ? "OAuth" : "API Key"}
    </span>
  );
}

export function ProviderTypeBadge({ providerType }: { providerType: string }) {
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs border ${
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
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs ${
      isOnline
        ? "bg-green-100 text-green-700 border border-green-200 dark:bg-green-950/40 dark:text-green-400 dark:border-green-800/40"
        : "bg-muted text-muted-foreground border border-border"
    }`}>
      <span className={`w-1.5 h-1.5 rounded-full ${isOnline ? "bg-green-500" : "bg-muted-foreground"}`} />
      {status}
    </span>
  );
}
