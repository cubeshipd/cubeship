import type { ReactNode } from "react";
import { Wordmark } from "@/components/brand";

// The shell both unauthenticated screens wear: signing in, and claiming
// an instance that has never been signed into. They are the same moment
// from the visitor's side, so they are the same layout — the brand panel
// is written once here and neither page can drift from it.
//
// Below `lg` the panel is gone rather than stacked: half a piece of
// artwork above a form is worse than no artwork.
export function AuthLayout({
  title,
  description,
  children,
  footer,
}: {
  title: string;
  description: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="flex min-h-screen bg-card">
      <BrandPanel />

      <main className="flex flex-1 items-center justify-center px-6 py-12">
        <div className="w-full max-w-[22rem]">
          <Wordmark className="mb-10 text-sm lg:hidden" markClassName="size-6" />

          <div className="hud-frame border border-border bg-background/60 p-6">
            <h1 className="text-lg font-bold tracking-[0.16em] uppercase">{title}</h1>
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{description}</p>

            <div className="mt-7">{children}</div>
          </div>

          {footer && (
            <div className="mt-5 font-mono text-[11px] leading-relaxed text-subtle-foreground">
              {footer}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function BrandPanel() {
  return (
    <aside className="relative hidden w-[44%] shrink-0 flex-col justify-between overflow-hidden border-r border-border-strong bg-background p-12 lg:flex xl:w-[48%]">
      {/* The grid is faded out at the edges so it reads as texture
          rather than as a table nobody can click. */}
      <div
        aria-hidden="true"
        className="absolute inset-0 bg-grid"
        style={{
          maskImage: "radial-gradient(120% 90% at 30% 20%, #000 0%, transparent 72%)",
          WebkitMaskImage: "radial-gradient(120% 90% at 30% 20%, #000 0%, transparent 72%)",
        }}
      />
      <div aria-hidden="true" className="absolute inset-0 bg-scanlines opacity-60" />
      <div
        aria-hidden="true"
        className="absolute -bottom-32 -left-40 size-[42rem] blur-3xl"
        style={{
          background:
            "radial-gradient(circle, color-mix(in srgb, var(--primary) 30%, transparent) 0%, transparent 70%)",
        }}
      />
      {/* The magenta sits opposite the cyan so the panel has two light
          sources rather than one wash. */}
      <div
        aria-hidden="true"
        className="absolute -top-40 -right-32 size-[30rem] blur-3xl"
        style={{
          background:
            "radial-gradient(circle, color-mix(in srgb, var(--magenta) 22%, transparent) 0%, transparent 70%)",
        }}
      />

      <Wordmark className="relative text-sm" markClassName="size-6" />

      <div className="relative max-w-md">
        <h2 className="text-3xl leading-[1.15] font-bold tracking-tight text-balance uppercase">
          Your own platform,
          <br />
          <span className="text-primary text-glow">on one machine.</span>
        </h2>
        <p className="mt-5 text-sm leading-relaxed text-muted-foreground">
          Cubeship runs your apps on a single VPS — build, deploy, route and renew certificates,
          from one binary you installed with one command.
        </p>

        <ul className="mt-8 space-y-2.5">
          <Point>
            <code className="text-foreground">docker push</code> is the deploy
          </Point>
          <Point>TLS and routing handled by Traefik, no DNS busywork</Point>
          <Point>One org, one project, one environment — or a hundred</Point>
        </ul>
      </div>

      <p className="relative font-mono text-[11px] tracking-[0.18em] text-subtle-foreground uppercase">
        self-hosted · no external services
      </p>
    </aside>
  );
}

function Point({ children }: { children: ReactNode }) {
  return (
    <li className="flex items-start gap-3 text-sm text-muted-foreground">
      <span
        aria-hidden="true"
        className="mt-[0.4rem] size-1.5 shrink-0 bg-primary shadow-[0_0_8px_var(--primary)]"
      />
      <span>{children}</span>
    </li>
  );
}
