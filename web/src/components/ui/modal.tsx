"use client";

import { useEffect, useRef } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export function Modal({
  open,
  onClose,
  children,
  className,
  title,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
  className?: string;
  title?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      ref={ref}
      onClick={(e) => { if (e.target === ref.current) onClose(); }}
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50"
    >
      <div
        role="dialog"
        aria-modal="true"
        className={cn("relative w-full max-w-lg rounded-xl border border-border bg-popover shadow-2xl", className)}
      >
        {title && (
          <div className="flex items-center justify-between px-5 py-3 border-b border-border">
            <h2 className="text-base font-semibold">{title}</h2>
            <button onClick={onClose} className="p-1 rounded-md hover:bg-muted text-muted-foreground">
              <X className="h-4 w-4" />
            </button>
          </div>
        )}
        {!title && (
          <button onClick={onClose} className="absolute right-3 top-3 p-1 rounded-md hover:bg-muted text-muted-foreground z-10">
            <X className="h-4 w-4" />
          </button>
        )}
        <div className="p-5">{children}</div>
      </div>
    </div>
  );
}
