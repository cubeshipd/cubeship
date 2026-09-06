import { cn } from "cn";

// Every state the daemon reports, and the one place its colour is
// decided. A status with no entry here is shown plainly rather than
// guessed at — a new deployment state should look unfamiliar, not
// green.
type Tone = { dot: string; text: string; edge: string; pulse?: boolean };

const tones: Record<string, Tone> = {
  running: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  succeeded: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  deploying: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  building: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  pending: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  failed: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  // A registry's three answers. Unauthorized is red rather than amber
  // because it is the one somebody has to act on: nothing recovers a
  // revoked key on its own, and the next deploy that pulls will fail.
  available: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  unauthorized: {
    dot: "bg-destructive",
    text: "text-destructive",
    edge: "border-destructive/40",
  },
  unreachable: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40" },
  checking: {
    dot: "bg-subtle-foreground",
    text: "text-muted-foreground",
    edge: "border-border-strong",
    pulse: true,
  },
  // A certificate's three. Traefik renews thirty days before expiry, so
  // a certificate still inside two weeks is one whose renewal is not
  // working — amber, not green — and an expired one is already failing
  // handshakes.
  valid: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  expiring: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40" },
  expired: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  stopped: {
    dot: "bg-subtle-foreground",
    text: "text-muted-foreground",
    edge: "border-border-strong",
  },
};

const unknown: Tone = {
  dot: "bg-subtle-foreground",
  text: "text-muted-foreground",
  edge: "border-border-strong",
};

export function statusTone(value: string): Tone {
  return tones[value] ?? unknown;
}

// The lamp on its own, for places that count states rather than name
// one — a project card summarising the apps inside it.
export function StatusDot({ value, className }: { value: string; className?: string }) {
  const t = statusTone(value);
  return (
    <span
      className={cn(
        "size-1.5 shrink-0 rounded-full shadow-[0_0_6px_currentColor]",
        t.dot,
        t.pulse && "animate-pulse",
        className,
      )}
    />
  );
}

export function StatusBadge({ value, className }: { value: string; className?: string }) {
  const t = statusTone(value);
  return (
    <span
      className={cn(
        "inline-flex h-5 items-center gap-1.5 border bg-background px-2 font-mono text-[10px] tracking-[0.14em] uppercase",
        t.edge,
        t.text,
        className,
      )}
    >
      <StatusDot value={value} />
      {value}
    </span>
  );
}
