import { cn } from "cn";

// The mark is the logo's cube, drawn here rather than imported so it
// takes the palette's accent and the glow around it: an isometric cube
// as a hologram — the nine visible edges lit, the three hidden ones
// faint through the faces. The same geometry is in public/logo/mark.svg
// at full size.
export function CubeMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true" className={cn("size-6", className)}>
      <title>Cubeship</title>
      <path d="M12 2 21 6.9 12 11.8 3 6.9z" className="fill-primary/16" />
      <path d="M3 6.9 12 11.8V22l-9-4.9z" className="fill-primary/8" />
      <path d="M12 11.8 21 6.9v10.2L12 22z" className="fill-primary/4" />
      <path
        d="M12 11.8V2M12 11.8 3 17.1M12 11.8l9 5.3"
        className="stroke-primary/25"
        strokeWidth=".7"
      />
      <path
        d="M12 2 21 6.9v10.2L12 22l-9-4.9V6.9z"
        className="stroke-primary"
        strokeWidth="1.2"
        strokeLinejoin="miter"
      />
      <path d="M3 6.9 12 11.8l9-4.9M12 11.8V22" className="stroke-primary" strokeWidth="1.2" />
    </svg>
  );
}

// The wordmark. Uppercase and widely tracked — it is a plate on a
// machine, not a sentence.
export function Wordmark({
  className,
  markClassName,
}: {
  className?: string;
  markClassName?: string;
}) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <CubeMark className={cn("drop-shadow-[0_0_10px_var(--primary)]", markClassName)} />
      <span className="font-semibold tracking-[0.26em] uppercase">cubeship</span>
    </span>
  );
}
