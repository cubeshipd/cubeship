"use client";

import Image, { type StaticImageData } from "next/image";
import { useState } from "react";
import app from "@/images/screens/app.png";
import backups from "@/images/screens/backups.png";
import overview from "@/images/screens/overview.png";
import { cn } from "@/lib/cn";

// Captured from the dashboard on invented data, at 1600 wide and twice
// the density, so a Retina screen sees the lines the interface is made of.
const screens: { key: string; label: string; caption: string; image: StaticImageData }[] = [
  {
    key: "overview",
    label: "Overview",
    caption:
      "What the machine underneath is doing — CPU, memory, disk and the bytes over its own interfaces, sampled every 30 seconds.",
    image: overview,
  },
  {
    key: "app",
    label: "An app",
    caption:
      "What one container is using, and every deploy it has had: whether each succeeded, and why it failed if it did.",
    image: app,
  },
  {
    key: "backups",
    label: "Backups",
    caption:
      "One row per database, worst first: never backed up, failing, on this machine, protected. A glance is enough.",
    image: backups,
  },
];

export function Screens() {
  const [current, setCurrent] = useState(screens[0]);

  return (
    <section id="screens" className="border-border border-b">
      <div className="mx-auto max-w-6xl px-6 py-20">
        <div className="flex flex-wrap items-end justify-between gap-6">
          <div>
            <p className="label text-primary">
              <span className="mr-2 inline-block h-3 w-0.5 bg-primary align-middle" />
              The dashboard
            </p>
            <h2 className="mt-4 max-w-2xl font-semibold text-3xl tracking-tight sm:text-4xl">
              A console, not a control panel.
            </h2>
          </div>
          <div role="tablist" className="flex border border-border">
            {screens.map((s) => (
              <button
                key={s.key}
                type="button"
                role="tab"
                aria-selected={s.key === current.key}
                onClick={() => setCurrent(s)}
                className={cn(
                  "label px-4 py-2.5 transition-colors",
                  s.key === current.key
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>
        <figure className="mt-10">
          <div className="hud-frame neon-edge border border-border bg-card">
            <Image
              src={current.image}
              alt={`The ${current.label.toLowerCase()} screen of the dashboard`}
              sizes="(min-width: 1152px) 1104px, 100vw"
              priority={current.key === "overview"}
              className="block w-full"
            />
          </div>
          <figcaption className="mt-4 text-muted-foreground text-sm leading-relaxed">
            {current.caption}
          </figcaption>
        </figure>
      </div>
    </section>
  );
}
