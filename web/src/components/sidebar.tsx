"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import Image from "next/image";
import {
  LayoutDashboard,
  Server,
  Sparkles,
  ScrollText,
  Layers,
  Globe,
  Wrench,
  Settings,
  ChevronLeft,
  ChevronRight,
  Sun,
  Moon,
  BookOpen,
} from "lucide-react";
import { useTheme } from "@/components/theme-provider";
import { cn } from "@/lib/utils";
import { useLanguage } from "@/lib/language-context";

const STORAGE_KEY = "paap-sidebar-collapsed";

const navItems = [
  { href: "/", label: "Dashboard", icon: LayoutDashboard },
  { href: "/providers", label: "Providers", icon: Server },
  { href: "/tools", label: "Tools", icon: Wrench },
  { href: "/skills", label: "Compression", icon: Sparkles },
  { href: "/logs", label: "Logs", icon: ScrollText },
  { href: "/groups", label: "Groups", icon: Layers },
  { href: "/proxy", label: "Proxy", icon: Globe },
  { href: "/settings", label: "Settings", icon: Settings },
];

export function Sidebar() {
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(false);
  const [mounted, setMounted] = useState(false);
  const { theme, toggle } = useTheme();
  const { t } = useLanguage();

  const navItems = [
    { href: "/", label: t("nav.dashboard"), icon: LayoutDashboard },
    { href: "/providers", label: t("nav.providers"), icon: Server },
    { href: "/tools", label: t("nav.tools"), icon: Wrench },
    { href: "/skills", label: "Compression", icon: Sparkles },
    { href: "/logs", label: t("nav.logs"), icon: ScrollText },
    { href: "/groups", label: t("nav.groups"), icon: Layers },
    { href: "/proxy", label: t("nav.proxy"), icon: Globe },
    { href: "/settings", label: t("nav.settings"), icon: Settings },
  ];

  useEffect(() => {
    const saved = typeof window !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
    if (saved !== null) setCollapsed(saved === "true");
    setMounted(true);
  }, []);

  const toggleCollapsed = () => {
    const next = !collapsed;
    setCollapsed(next);
    localStorage.setItem(STORAGE_KEY, String(next));
  };

  return (
    <aside
      className={cn(
        "flex flex-col shrink-0 transition-[width] duration-200 ease-in-out",
        "border-r border-border backdrop-blur-xl bg-sidebar",
        collapsed ? "w-16" : "w-56"
      )}
      aria-label="Primary sidebar"
    >
      <div className="flex items-center gap-2 px-4 py-4 border-b border-border">
        <Image
          src="/assets/logo.svg"
          alt="PAAP"
          width={collapsed ? 28 : 120}
          height={28}
          className="shrink-0 h-auto w-auto"
          priority
        />
      </div>

      <nav className="flex-1 px-2 py-3 space-y-0.5">
        {navItems.map(({ href, label, icon: Icon }) => {
          const active = pathname === href || (href !== "/" && pathname.startsWith(href));
          return (
            <Link
              key={href}
              href={href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-all duration-200",
                collapsed && "justify-center",
                active
                  ? "bg-primary/10 text-primary font-medium"
                  : "text-muted-foreground hover:bg-secondary hover:text-foreground"
              )}
              title={collapsed ? label : undefined}
            >
              {active && (
                <span className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-4 rounded-r-full bg-primary" />
              )}
              <Icon className="w-5 h-5 shrink-0" aria-hidden="true" />
              {!collapsed && <span className="whitespace-nowrap">{label}</span>}
            </Link>
          );
        })}
      </nav>

      <div className="px-2 pb-2 space-y-1">
        <button
          onClick={toggle}
          className={cn(
            "flex items-center gap-3 px-3 py-2 rounded-lg text-sm w-full transition-colors",
            collapsed ? "justify-center" : "",
            "text-muted-foreground hover:bg-secondary hover:text-foreground"
          )}
          title={collapsed ? (theme === "dark" ? "Light mode" : "Dark mode") : undefined}
        >
          {theme === "dark" ? (
            <Sun className="w-5 h-5 shrink-0" />
          ) : (
            <Moon className="w-5 h-5 shrink-0" />
          )}
          {!collapsed && <span>{theme === "dark" ? "Light mode" : "Dark mode"}</span>}
        </button>

        <Link
          href="/docs"
          className={cn(
            "flex items-center gap-3 px-3 py-2 rounded-lg text-sm w-full transition-colors",
            collapsed ? "justify-center" : "",
            "text-muted-foreground hover:bg-secondary hover:text-foreground"
          )}
          title={collapsed ? "Docs" : undefined}
        >
          <BookOpen className="w-5 h-5 shrink-0" />
          {!collapsed && <span>Docs</span>}
        </Link>

        <button
          onClick={toggleCollapsed}
          aria-expanded={mounted ? !collapsed : true}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className={cn(
            "flex items-center justify-center py-2.5 rounded-lg w-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
          )}
        >
          {collapsed ? (
            <ChevronRight className="w-4 h-4" />
          ) : (
            <ChevronLeft className="w-4 h-4" />
          )}
        </button>
      </div>
    </aside>
  );
}
