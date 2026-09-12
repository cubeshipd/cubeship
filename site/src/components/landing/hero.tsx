import { ArrowRight } from "lucide-react";
import Link from "next/link";
import { InstallCommand } from "@/components/install-command";
import { githubUrl } from "@/lib/shared";

export function Hero() {
  return (
    <section className="relative overflow-hidden border-border border-b">
      <div className="bg-grid absolute inset-0 [mask-image:radial-gradient(ellipse_at_center,black_20%,transparent_75%)]" />
      <div className="relative mx-auto flex max-w-6xl flex-col items-center px-6 pt-24 pb-20 text-center">
        <p className="label text-primary">
          self-hosted <span className="text-magenta">·</span> one VPS or a whole cluster{" "}
          <span className="text-magenta">·</span> run by you or your agent
        </p>
        <h1 className="mt-5 max-w-3xl font-semibold text-4xl leading-tight tracking-tight sm:text-6xl">
          A PaaS you run on your own servers.
        </h1>
        <p className="mt-6 max-w-2xl text-lg text-muted-foreground leading-relaxed">
          Push an image and it is live, with HTTPS and a database beside it. Add a second machine
          and it is a cluster. Hand the API key to an agent and it runs the whole thing.
        </p>
        <div className="mt-10 w-full max-w-xl">
          <InstallCommand />
        </div>
        <p className="mt-3 text-subtle-foreground text-xs">
          One command on a fresh Debian or Ubuntu box. It installs Docker, pulls two images and
          prints the address to open.
        </p>
        <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
          <Link
            href="/docs"
            className="neon-edge inline-flex items-center gap-2 bg-primary px-5 py-2.5 font-semibold text-primary-foreground text-sm uppercase tracking-[0.18em] transition-colors hover:bg-primary/90"
          >
            Read the docs <ArrowRight className="size-4" />
          </Link>
          <a
            href={githubUrl}
            className="inline-flex items-center gap-2 border border-border-strong px-5 py-2.5 font-semibold text-foreground text-sm uppercase tracking-[0.18em] transition-colors hover:border-primary hover:text-primary"
          >
            GitHub
          </a>
        </div>
      </div>
    </section>
  );
}
