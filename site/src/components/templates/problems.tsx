import type { Diagnostic } from "@/lib/template";

const severityLabel: Record<Diagnostic["severity"], string> = {
  error: "text-magenta",
  warning: "text-warning",
  info: "text-primary",
};

// One list for both the author's own typing and whatever the server sent
// back on a 422 — a diagnostic is one shape everywhere, so there is
// nothing here that has to know which source it came from.
export function Problems({
  diagnostics,
  onJump,
}: {
  diagnostics: Diagnostic[];
  onJump: (range: NonNullable<Diagnostic["range"]>) => void;
}) {
  if (diagnostics.length === 0) {
    return <p className="label px-1 text-fd-muted-foreground">No problems found</p>;
  }

  return (
    <ul className="hud-frame divide-y divide-fd-border border border-fd-border">
      {diagnostics.map((diagnostic) => (
        <li key={`${diagnostic.path.join(".")}-${diagnostic.code}-${diagnostic.message}`}>
          <button
            type="button"
            disabled={!diagnostic.range}
            onClick={() => diagnostic.range && onJump(diagnostic.range)}
            className="flex w-full flex-col gap-1 px-3 py-2 text-left text-sm hover:bg-fd-accent disabled:cursor-default disabled:hover:bg-transparent"
          >
            <span className={`label ${severityLabel[diagnostic.severity]}`}>
              {diagnostic.severity} · {diagnostic.code}
            </span>
            <span className="text-fd-foreground">{diagnostic.message}</span>
            {diagnostic.hint ? (
              <span className="text-fd-muted-foreground text-xs">{diagnostic.hint}</span>
            ) : null}
          </button>
        </li>
      ))}
    </ul>
  );
}
