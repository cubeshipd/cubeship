import { cn } from "cn";

// Every state the daemon reports, and the one place its colour is
// decided. A status with no entry here is shown plainly rather than
// guessed at — a new deployment state should look unfamiliar, not
// green.
type Tone = { dot: string; text: string; edge: string; pulse?: boolean };

const tones: Record<string, Tone> = {
  running: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  // A server in the cluster that is calling in. Green for the same
  // reason `running` is: it is the state where nothing needs doing.
  ready: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  succeeded: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  deploying: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  building: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  pending: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  // A dump in flight. Amber and pulsing rather than green, because a
  // backup that is still being taken is not one yet — and `running`,
  // which would be the natural word, is already a healthy container
  // here and is painted green.
  taking: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40", pulse: true },
  failed: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  // Some of an app's machines serving and some not. Amber rather than
  // red because the name still answers — what is lost is the headroom
  // that was the reason for the second machine — and rather than green
  // because something is down and nothing will notice on its own. It
  // cannot happen to an app on one machine.
  degraded: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40" },
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
  // A firewall rule's verb. Allow is green because it is what makes
  // something work; deny and reject are red because they are what stops
  // it — the colour is about what the rule does, not about whether
  // having it is good news.
  allow: { dot: "bg-success", text: "text-success", edge: "border-success/40" },
  deny: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  reject: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  expiring: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40" },
  expired: { dot: "bg-destructive", text: "text-destructive", edge: "border-destructive/40" },
  stopped: {
    dot: "bg-subtle-foreground",
    text: "text-muted-foreground",
    edge: "border-border-strong",
  },
  // A name routed here with no certificate behind it. Amber and not
  // red: it is the ordinary state for the minute after a deploy, and
  // the row carries the reason for when it is not.
  waiting: { dot: "bg-warning", text: "text-warning", edge: "border-warning/40" },
  // Served by another machine in the cluster, which holds its own. Not
  // a problem, and the only entry in the missing half that is not.
  elsewhere: {
    dot: "bg-subtle-foreground",
    text: "text-muted-foreground",
    edge: "border-border-strong",
  },
  // A certificate nothing answers at any more. Grey rather than amber:
  // it is still valid and still renewing, and the only thing wrong with
  // it is that it is spending a weekly allowance on a name nobody uses.
  unused: {
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
export function StatusDot({
  value,
  className,
  // What it means, on hover. A lamp on its own says a colour; where the
  // word is not beside it, this is what carries the word.
  title,
}: {
  value: string;
  className?: string;
  title?: string;
}) {
  const t = statusTone(value);
  return (
    <span
      title={title}
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
        "inline-flex h-5 items-center gap-1.5 border bg-background px-2 font-mono text-[10px] uppercase",
        t.edge,
        t.text,
        className,
      )}
    >
      <StatusDot value={value} />
      {/* Centred by eye rather than by box, which is not the same thing
          for a line of capitals.
          
          `items-center` centres the text's *line box*, and that box
          holds descender room no capital uses — so the ink sat about a
          pixel above the dot beside it, which is exactly where the eye
          looks for the difference. `leading-none` takes the slack out
          and the nudge spends what is left.
          
          The negative margin is the other half of the same problem:
          letter-spacing is applied after the last character too, so a
          tracked word inside equal padding is not equally padded. */}
      <span className="-mr-[0.14em] translate-y-[0.5px] leading-none tracking-[0.14em]">
        {value}
      </span>
    </span>
  );
}
