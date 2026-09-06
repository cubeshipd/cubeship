"use client";

import { cn } from "cn";
import { useId } from "react";
import { CopyButton } from "@/components/copy-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// A value you read and copy, never edit.
//
// A read-only field rather than a line of text, and the difference is
// not decoration: a connection string is long, and a field scrolls
// inside itself and lets you select it with one triple-click, where a
// wrapped paragraph gives you three lines and a chance to miss one.
//
// readOnly rather than disabled. A disabled input cannot be focused,
// selected or copied from with the keyboard, which is most of what this
// is for.
export function CopyField({
  label,
  value,
  hint,
  // secret hides the value until it is asked for. What is hidden is
  // still copyable — the point of the button is that nobody has to read
  // a password to use it.
  masked = false,
  fieldClassName,
  className,
}: {
  label: string;
  value: string;
  hint?: React.ReactNode;
  masked?: boolean;
  fieldClassName?: string;
  className?: string;
}) {
  const id = useId();
  return (
    <div className={cn("space-y-2", fieldClassName)}>
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <div className="relative">
        <Input
          id={id}
          readOnly
          spellCheck={false}
          type={masked ? "password" : "text"}
          value={value}
          // Room on the right for the button, which is inside the field
          // rather than beside it: two of these side by side would
          // otherwise have their buttons at different heights whenever
          // one label wrapped and the other did not.
          className={cn("h-10 px-3 pr-10 text-sm", className)}
          onFocus={(e) => e.currentTarget.select()}
        />
        <CopyButton
          value={value}
          label={`Copy ${label.toLowerCase()}`}
          className="absolute top-1/2 right-1 -translate-y-1/2"
        />
      </div>
      {hint && <p className="text-xs leading-relaxed text-subtle-foreground">{hint}</p>}
    </div>
  );
}
