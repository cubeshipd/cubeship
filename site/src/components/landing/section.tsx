import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

// Every section below the hero has the same skeleton: a label, a
// title, a line under it, and whatever the section is. The label is
// the sidebar's own — mono, uppercase, tracked.
export function Section({
  id,
  label,
  title,
  lede,
  children,
  className,
}: {
  id?: string;
  label: string;
  title: string;
  lede?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section id={id} className={cn("border-border border-b", className)}>
      <div className="mx-auto max-w-6xl px-6 py-20">
        <p className="label text-primary">
          <span className="mr-2 inline-block h-3 w-0.5 bg-primary align-middle" />
          {label}
        </p>
        <h2 className="mt-4 max-w-2xl font-semibold text-3xl tracking-tight sm:text-4xl">
          {title}
        </h2>
        {lede ? (
          <p className="mt-4 max-w-2xl text-lg text-muted-foreground leading-relaxed">{lede}</p>
        ) : null}
        <div className="mt-12">{children}</div>
      </div>
    </section>
  );
}

// A block of terminal, the way the README shows one.
export function Terminal({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <div className="hud-frame min-w-0 border border-border bg-card">
      {title ? (
        <div className="label border-border border-b px-4 py-2 text-subtle-foreground">{title}</div>
      ) : null}
      <pre className="overflow-x-auto px-4 py-4 font-mono text-[13px] text-foreground leading-relaxed">
        {children}
      </pre>
    </div>
  );
}

export function Prompt({ children }: { children: ReactNode }) {
  return (
    <>
      <span className="text-magenta select-none">$ </span>
      {children}
      {"\n"}
    </>
  );
}

export function Comment({ children }: { children: ReactNode }) {
  return <span className="text-subtle-foreground">{children}</span>;
}
