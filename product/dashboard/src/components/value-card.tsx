import { cn } from "cn";
import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";

// A label over one machine-readable value: a push command, a registry
// host, an image reference, an API key. The value is mono and wraps,
// because these are read character by character and are often longer
// than the card.
export function ValueCard({
  label,
  value,
  className,
}: {
  label: ReactNode;
  value: ReactNode;
  className?: string;
}) {
  return (
    <Card size="sm" className={cn("mb-4 border-l-2 border-l-primary/60", className)}>
      <CardContent>
        <div className="text-[11px] tracking-[0.12em] text-muted-foreground uppercase">{label}</div>
        <div className="mt-2 font-mono text-sm break-all text-foreground">{value}</div>
      </CardContent>
    </Card>
  );
}
