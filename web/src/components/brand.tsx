import { cn } from "cn";

// The mark is an isometric cube drawn as a wireframe: three visible
// edges lit in the interface accent, the hidden ones implied. It is
// drawn rather than imported so it inherits the glow around it and
// scales with the type it sits next to.
export function CubeMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true" className={cn("size-6", className)}>
      <title>Cubeship</title>
      <path d="M12 2 21 6.9v10.2L12 22l-9-4.9V6.9z" className="fill-primary/8" />
      <path d="M12 2 21 6.9 12 11.8 3 6.9z" className="fill-primary/22" />
      <path
        d="M12 2 21 6.9v10.2L12 22l-9-4.9V6.9z"
        className="stroke-primary"
        strokeWidth="1.2"
        strokeLinejoin="miter"
      />
      <path
        d="M3 6.9 12 11.8l9-4.9M12 11.8V22"
        className="stroke-primary/70"
        strokeWidth="1.2"
        strokeLinejoin="miter"
      />
      {/* The one magenta element in the interface: the cube's near
          corner, lit. Nothing you have to read is ever this colour. */}
      <path d="M12 2 21 6.9" className="stroke-magenta" strokeWidth="1.2" />
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
