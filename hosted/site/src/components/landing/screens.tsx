"use client";

import {
  ArrowUpRight,
  ChartNoAxesCombined,
  Layers,
  LoaderCircle,
  Maximize2,
  Minimize2,
  Play,
  RotateCcw,
  ShieldCheck,
  X,
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import app from "@/images/screens/app.png";
import backups from "@/images/screens/backups.png";
import overview from "@/images/screens/overview.png";

const screens = [
  {
    key: "overview",
    route: "/",
    label: "Infrastructure",
    icon: ChartNoAxesCombined,
    title: "The whole picture. At a glance.",
    caption:
      "CPU, memory, disk and network. Know what your servers are doing, without piecing together another stack of tools.",
    image: overview,
  },
  {
    key: "app",
    route: "/projects/web/production/api",
    label: "Deployments",
    icon: Layers,
    title: "Every deploy. Every detail.",
    caption:
      "Follow your releases, read your logs and see what each app is using. Everything you need to understand what is running.",
    image: app,
  },
  {
    key: "backups",
    route: "/backups",
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
  const windowRef = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const [demoURL, setDemoURL] = useState<string | null>(null);
  const [generation, setGeneration] = useState(0);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [expanded, setExpanded] = useState(false);
  const live = demoURL !== null;

  useEffect(() => {
    const receive = (event: MessageEvent) => {
      if (event.origin !== window.location.origin || event.source !== frame.current?.contentWindow)
        return;
      if (event.data?.type === "cubeship:demo:ready") setState("ready");
    };
    const fullscreen = () => setExpanded(document.fullscreenElement === windowRef.current);
    window.addEventListener("message", receive);
    document.addEventListener("fullscreenchange", fullscreen);
    return () => {
      window.removeEventListener("message", receive);
      document.removeEventListener("fullscreenchange", fullscreen);
    };
  }, []);

  useEffect(() => {
    if (!live || state !== "loading" || generation === 0) return;
    const timer = window.setTimeout(() => setState("error"), 25_000);
    return () => window.clearTimeout(timer);
  }, [live, state, generation]);

  function start() {
    setState("loading");
    setGeneration((value) => value + 1);
    setDemoURL(`/demo${current.route}`);
  }

  function select(index: number) {
    setSelected(index);
    if (live)
      frame.current?.contentWindow?.postMessage(
        { type: "cubeship:demo:navigate", screen: screens[index].key },
        window.location.origin,
      );
  }

  async function toggleFullscreen() {
    if (document.fullscreenElement) await document.exitFullscreen();
    else if (windowRef.current?.requestFullscreen) await windowRef.current.requestFullscreen();
    else if (demoURL) window.open(demoURL, "_blank", "noopener,noreferrer");
  }
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
      <div ref={windowRef} className={`product-window ${live ? "product-window-live" : ""}`}>
        <div className="window-bar">
          <div className="window-dots">
            <i />
            <i />
            <i />
          </div>
          <span>
            <span className="status-light" /> cubeship / your infrastructure
          </span>
          <div className="demo-window-actions">
            {live ? (
              <>
                <span className="demo-live-label">Live demo</span>
                <button type="button" onClick={start} aria-label="Reset demo" title="Reset demo">
                  <RotateCcw size={15} />
                </button>
                <button
                  type="button"
                  onClick={() => void toggleFullscreen()}
                  aria-label={expanded ? "Exit fullscreen" : "Expand demo"}
                  title="Expand demo"
                >
                  {expanded ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setDemoURL(null);
                    if (document.fullscreenElement) void document.exitFullscreen();
                  }}
                  aria-label="Close demo"
                  title="Close demo"
                >
                  <X size={16} />
                </button>
              </>
            ) : (
              <span className="window-version">Self hosted</span>
            )}
          </div>
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
              disabled={live && state !== "ready"}
              onClick={() => select(i)}
              onKeyDown={(event) => {
                let next = i;
                if (event.key === "ArrowRight") next = (i + 1) % screens.length;
                else if (event.key === "ArrowLeft")
                  next = (i + screens.length - 1) % screens.length;
                else if (event.key === "Home") next = 0;
                else if (event.key === "End") next = screens.length - 1;
                else return;
                event.preventDefault();
                select(next);
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
          // biome-ignore lint/a11y/noNoninteractiveTabindex: A tab panel without focusable children must be reachable from its tab.
          tabIndex={0}
          className="product-screen"
        >
          {live && (
            <iframe
              key={generation}
              ref={frame}
              src={demoURL ?? undefined}
              title="Cubeship interactive dashboard demo"
              className="demo-frame"
              sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"
              allow="fullscreen; clipboard-write"
              onError={() => setState("error")}
            />
          )}
          {(!live || state !== "ready") && (
            <Image
              src={current.image}
              alt={`Cubeship dashboard showing ${current.label.toLowerCase()} with demonstration data`}
              sizes="(min-width: 1440px) 1230px, 94vw"
              className="block w-full"
            />
          )}
          {!live && (
            <div className="demo-entry">
              <div>
                <p>Your next control center. Try it here.</p>
                <span>Real dashboard. Sample data. No sign-up.</span>
              </div>
              <button
                type="button"
                className="button-primary"
                onClick={start}
                data-track="demo-start"
              >
                <Play size={16} /> Explore the live demo
              </button>
            </div>
          )}
          {live && state !== "ready" && (
            <div className="demo-loading" role="status">
              {state === "loading" ? (
                <>
                  <LoaderCircle size={24} className="demo-spinner" />
                  <p>Preparing your playground…</p>
                  <span>Starting the real Cubeship dashboard.</span>
                </>
              ) : (
                <>
                  <p>The demo couldn't start.</p>
                  <span>Try again, or keep exploring the screenshots.</span>
                  <button type="button" className="button-primary" onClick={start}>
                    Try again
                  </button>
                  <button type="button" onClick={() => setDemoURL(null)}>
                    Back to screenshots
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      </div>
      <div className="screen-caption">
        <div>
          <h3>{live ? "Make yourself at home." : current.title}</h3>
          <p>
            {live
              ? "Open projects, deploy an app, explore your servers. Everything here is simulated and resets when you reload."
              : current.caption}
          </p>
        </div>
        <Link href="/docs">
          Explore the docs <ArrowUpRight size={16} />
        </Link>
      </div>
    </section>
  );
}
