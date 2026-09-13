"use client";

import { Loader2Icon } from "lucide-react";
import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";

// A button that is doing something. `busy` disables it and shows the
// spinner in one prop, because the two coming apart is how a form ends
// up submittable twice.
export function ActionButton({
  busy = false,
  children,
  disabled,
  ...props
}: ComponentProps<typeof Button> & { busy?: boolean }) {
  return (
    <Button disabled={disabled || busy} {...props}>
      {busy && <Loader2Icon className="animate-spin" />}
      {children}
    </Button>
  );
}
