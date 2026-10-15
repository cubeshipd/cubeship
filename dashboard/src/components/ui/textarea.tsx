import type * as React from "react";
import { cn } from "cn";

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "field-sizing-content min-h-16 w-full min-w-0 resize-y border border-input bg-transparent px-2.5 py-1.5 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        className,
      )}
      {...props}
    />
  );
}

export { Textarea };
