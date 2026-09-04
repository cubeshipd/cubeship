import type { ReactNode } from "react";

// The bottom of a settings screen, where the actions that cannot be
// undone live — away from the fields you edit and save without thinking.
export function DangerZone({ children }: { children: ReactNode }) {
  return (
    <section className="mt-10 border border-destructive/30">
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
