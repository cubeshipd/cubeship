"use client";

import { ChevronDownIcon } from "lucide-react";
import { type ReactNode, useId, useState } from "react";
import { Button } from "@/components/ui/button";

export function WorkspaceActions({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const id = useId();

  return (
    <section className="mb-3 lg:mb-5">
      <h2 className="mb-2 hidden px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground lg:block">
        Actions
      </h2>
      <Button
        variant="outline"
        className="mb-2 w-full justify-between lg:hidden"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((value) => !value)}
      >
        Actions
        <ChevronDownIcon className={open ? "rotate-180" : undefined} />
      </Button>
      <div id={id} className={`${open ? "grid" : "hidden"} gap-1 lg:grid`}>
        {children}
      </div>
    </section>
  );
}
