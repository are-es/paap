"use client";

import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const toggleVariants = cva(
  "relative inline-flex shrink-0 cursor-pointer items-center rounded-full transition-all duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50",
  {
    variants: {
      size: {
        default: "h-6 w-11",
        sm: "h-4 w-7",
        lg: "h-7 w-14",
      },
    },
    defaultVariants: {
      size: "default",
    },
  }
);

export interface ToggleProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "onChange">,
    VariantProps<typeof toggleVariants> {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
}

const Toggle = React.forwardRef<HTMLButtonElement, ToggleProps>(
  ({ className, size, checked = false, onCheckedChange, disabled, ...props }, ref) => {
    const handleClick = () => {
      if (!disabled) {
        onCheckedChange?.(!checked);
      }
    };

    const thumbSize = size === "sm" ? "h-3 w-3" : size === "lg" ? "h-5 w-5" : "h-4 w-4";
    const thumbTranslate = size === "sm" ? "translate-x-3.5" : size === "lg" ? "translate-x-7.5" : "translate-x-5.5";

    return (
      <button
        ref={ref}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={handleClick}
        className={cn(
          toggleVariants({ size, className }),
          checked
            ? "bg-primary"
            : "bg-foreground/15"
        )}
        {...props}
      >
        <span
          className={cn(
            "pointer-events-none rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform duration-200",
            thumbSize,
            checked ? thumbTranslate : "translate-x-0.5"
          )}
        />
      </button>
    );
  }
);
Toggle.displayName = "Toggle";

export { Toggle, toggleVariants };
