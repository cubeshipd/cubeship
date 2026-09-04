import { cn } from "cn";

// What a page shows while it is waiting on something it has to go and
// ask for — a registry someone else runs, most of the time.
//
// It is a sweep along a rule rather than a grey block pulsing: depth
// here comes from 1px lines and glow, and the skeleton has to belong to
// the same interface as the thing it stands in for. Each placeholder is
// the width and height of the value it is waiting on, so nothing moves
// when the answer lands.

// One value: a host, a size, a count.
export function LoadingValue({ className }: { className?: string }) {
  return (
    <span className={cn("inline-block align-middle", className)}>
      <span className="scan-line block w-full" />
    </span>
  );
}

// A table waiting on its rows. `columns` keeps the placeholder in the
// same grid as the real rows, so the header above it does not shift.
export function LoadingRows({ rows = 3, columns = 1 }: { rows?: number; columns?: number }) {
  return (
    <>
      {Array.from({ length: rows }, (_, r) => (
        // The index is the identity here: these are positions, not data.
        // biome-ignore lint/suspicious/noArrayIndexKey: placeholders have no id
        <tr key={r} className="border-b border-border last:border-0">
          {Array.from({ length: columns }, (_, c) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: placeholders have no id
            <td key={c} className="px-4 py-3.5">
              <span
                className="scan-block block h-3"
                style={{
                  width: `${[70, 45, 30, 55][c % 4]}%`,
                  animationDelay: `${(r * columns + c) * 90}ms`,
                }}
              />
            </td>
          ))}
        </tr>
      ))}
    </>
  );
}

// A list of blocks, for a page whose content is not a table — the
// repositories on a registry's detail screen.
export function LoadingList({ rows = 4 }: { rows?: number }) {
  return (
    <div className="divide-y divide-border border border-border">
      {Array.from({ length: rows }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: placeholders have no id
        <div key={i} className="flex items-center justify-between gap-6 px-4 py-3.5">
          <span
            className="scan-block block h-3"
            style={{ width: `${[52, 38, 61, 44][i % 4]}%`, animationDelay: `${i * 110}ms` }}
          />
          <span
            className="scan-block block h-3 w-16 shrink-0"
            style={{ animationDelay: `${i * 110 + 55}ms` }}
          />
        </div>
      ))}
    </div>
  );
}

// What a page says while it waits, under the placeholder. A sweep with
// no words reads as broken on a wait long enough to notice, and asking
// someone else's registry is exactly that long.
export function LoadingNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="mt-3 flex items-center gap-2 font-mono text-[11px] tracking-[0.1em] text-subtle-foreground uppercase">
      <span className="size-1.5 animate-pulse rounded-full bg-primary shadow-[0_0_6px_var(--primary)]" />
      {children}
    </p>
  );
}
