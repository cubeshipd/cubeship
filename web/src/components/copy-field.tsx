"use client";

import { cn } from "cn";
import { useId } from "react";
import { CopyButton } from "@/components/copy-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// A value you read and copy, never edit.
//
// A field rather than a line of text, and the difference is not
// decoration: a connection string is long, and a field bounds it and
// keeps it on one line where a wrapped paragraph gives you three and a
// chance to miss one.
//
// **Disabled, and unselectable.** It was readOnly first, which focused
// on a click and selected itself — so clicking the *label* lit the ring
// and highlighted the value, which reads as an edit about to happen on
// something that cannot be edited. The button is how a value leaves
// this field; nothing here needs to take focus for that, and a
// selection nobody asked for is only ever half a connection string.
//
// The usual disabled dimming is turned off for these in globals.css:
// dimming says "unavailable for now", and this is not unavailable, it
// is final.
export function CopyField({
  label,
  value,
  hint,
  // masked hides the value until it is asked for. What is hidden is
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
          disabled
          data-copyable=""
          spellCheck={false}
          type={masked ? "password" : "text"}
          value={value}
          // Room on the right for the button, which sits inside the
          // field rather than beside it: two of these side by side
          // would otherwise have their buttons at different heights
          // whenever one label wrapped and the other did not.
          className={cn("h-10 px-3 pr-10 text-sm", className)}
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
