"use client";

import { cn } from "cn";
import { type ReactNode, useId } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// A labelled input, wired by id. Every form in the dashboard is a stack
// of these, which is the only reason the label and the field it names
// can never come apart.
export function TextField({
  label,
  hint,
  action,
  className,
  fieldClassName,
  ...props
}: React.ComponentProps<"input"> & {
  label: string;
  hint?: ReactNode;
  // Layout for the field as a whole — its width in a row of them.
  // `className` styles the input itself.
  fieldClassName?: string;
  // Something that belongs beside the label rather than under it — a
  // "forgot password", a unit, a link out.
  action?: ReactNode;
}) {
  const id = useId();
  return (
    <div className={cn("space-y-2", fieldClassName)}>
      <div className="flex items-baseline justify-between gap-3">
        <Label htmlFor={id} className="text-xs text-muted-foreground">
          {label}
        </Label>
        {action}
      </div>
      <Input id={id} className={cn("h-10 bg-background px-3 text-sm", className)} {...props} />
      {hint && <p className="text-xs leading-relaxed text-subtle-foreground">{hint}</p>}
    </div>
  );
}
