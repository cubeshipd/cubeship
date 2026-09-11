import { cn } from "cn";
import type { ReactNode } from "react";

// The bottom of a settings screen, where the actions that cannot be
// undone live — away from the fields you edit and save without thinking.
//
// **The gap above it belongs to whatever is above it**, not to this. It
// carried `mt-10` for the distance it wanted from a form, which was
// invisible while every settings screen had one — and a lie on the two
// that are now the delete and nothing else, where it pushed the only
// thing on the page a long way down from a rail it was already clear
// of. A caller that wants the distance says so.
export function DangerZone({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <section className={cn("border border-destructive/30", className)}>
      <h2 className="border-b border-destructive/30 bg-destructive/8 px-4 py-2.5 text-xs font-semibold tracking-[0.16em] text-destructive uppercase">
        Danger zone
      </h2>
      <div className="divide-y divide-destructive/20">{children}</div>
    </section>
  );
}

export function DangerAction({
  title,
  description,
  action,
}: {
  title: string;
  description: ReactNode;
  action: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-6 p-4">
      <div className="min-w-0">
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{description}</p>
      </div>
      <div className="shrink-0">{action}</div>
    </div>
  );
}
