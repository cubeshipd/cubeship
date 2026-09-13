import { ArrowRight } from "lucide-react";
import Link from "next/link";
import { FaultyTerminal } from "@/components/faulty-terminal";
import { InstallCommand } from "@/components/install-command";
import { githubUrl } from "@/lib/shared";

export function Hero() {
  return (
    <section className="relative overflow-hidden border-border border-b">
      <FaultyTerminal
        className="absolute inset-0 [mask-image:radial-gradient(ellipse_at_center,black_10%,transparent_72%)]"
        tint="#2de2e6"
        brightness={0.32}
        scale={1.6}
        digitSize={1.2}
        timeScale={0.35}
        scanlineIntensity={0.5}
        glitchAmount={0.6}
        flickerAmount={0.5}
        mouseStrength={0.12}
      />
      <div className="relative mx-auto flex max-w-6xl flex-col items-center px-6 pt-24 pb-20 text-center">
        <p className="label text-primary">
          self-hosted <span className="text-magenta">·</span> one VPS or a whole cluster{" "}
          <span className="text-magenta">·</span> run by you or your agent
        </p>
        <h1 className="mt-5 max-w-3xl font-semibold text-4xl leading-tight tracking-tight sm:text-6xl">
          Your servers, run like a platform.
        </h1>
        <p className="mt-6 max-w-2xl text-foreground/85 text-lg leading-relaxed">
          Apps, databases and their backups, object storage, certificates and a firewall — the whole
          platform, on servers you own. One machine or a cluster, one dashboard, one API.
        </p>
        <div className="mt-10 w-full max-w-xl">
          <InstallCommand />
        </div>
        <p className="mt-3 text-foreground/80 text-sm">
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
