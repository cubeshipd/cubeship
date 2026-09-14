"use client";

import { ArrowUpRight, ChartNoAxesCombined, Layers, ShieldCheck } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { useRef, useState } from "react";
import app from "@/images/screens/app.png";
import backups from "@/images/screens/backups.png";
import overview from "@/images/screens/overview.png";

const screens = [
  {
    key: "overview",
    label: "Infrastructure",
    icon: ChartNoAxesCombined,
    title: "The whole picture. At a glance.",
    caption:
      "CPU, memory, disk and network. Know what your servers are doing, without piecing together another stack of tools.",
    image: overview,
  },
  {
    key: "app",
    label: "Deployments",
    icon: Layers,
    title: "Every deploy. Every detail.",
    caption:
      "Follow your releases, read your logs and see what each app is using. Everything you need to understand what is running.",
    image: app,
  },
  {
    key: "backups",
    label: "Backups",
    icon: ShieldCheck,
    title: "Your data deserves a plan.",
    caption:
      "See which databases are protected, catch failed backups and restore from the same place you manage your data.",
    image: backups,
  },
];

export function Screens() {
  const [selected, setSelected] = useState(0);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const current = screens[selected];
  return (
    <section id="screens" className="product-showcase site-container">
      <div className="section-heading">
        <div>
          <p className="section-kicker">Meet your control center</p>
          <h2>
            Everything running.
            <br />
            Everything in reach.
          </h2>
        </div>
        <p>
          A real platform for the things you build.
          <br />
          One place to deploy, observe and operate.
        </p>
      </div>
      <div className="product-window">
        <div className="window-bar">
          <div className="window-dots">
            <i />
            <i />
            <i />
          </div>
          <span>
            <span className="status-light" /> cubeship / your infrastructure
          </span>
          <span className="window-version">Self hosted</span>
        </div>
        <div className="product-tabs" role="tablist" aria-label="Explore the dashboard">
          {screens.map((screen, i) => (
            <button
              ref={(element) => {
                buttons.current[i] = element;
              }}
              key={screen.key}
              type="button"
              role="tab"
              id={`tab-${screen.key}`}
              aria-controls={`panel-${screen.key}`}
              aria-selected={selected === i}
              tabIndex={selected === i ? 0 : -1}
              onClick={() => setSelected(i)}
              onKeyDown={(event) => {
                let next = i;
                if (event.key === "ArrowRight") next = (i + 1) % screens.length;
                else if (event.key === "ArrowLeft")
                  next = (i + screens.length - 1) % screens.length;
                else if (event.key === "Home") next = 0;
                else if (event.key === "End") next = screens.length - 1;
                else return;
                event.preventDefault();
                setSelected(next);
                buttons.current[next]?.focus();
              }}
            >
              <screen.icon size={15} />
              {screen.label}
            </button>
          ))}
        </div>
        <div
          role="tabpanel"
          id={`panel-${current.key}`}
          aria-labelledby={`tab-${current.key}`}
          tabIndex={0}
          className="product-screen"
        >
          <Image
            src={current.image}
            alt={`Cubeship dashboard showing ${current.label.toLowerCase()} with demonstration data`}
            sizes="(min-width: 1440px) 1230px, 94vw"
            className="block w-full"
          />
        </div>
      </div>
      <div className="screen-caption">
        <div>
          <h3>{current.title}</h3>
          <p>{current.caption}</p>
        </div>
        <Link href="/docs">
          Explore the docs <ArrowUpRight size={16} />
        </Link>
      </div>
    </section>
  );
}
