"use client";

import { SiGithub } from "@icons-pack/react-simple-icons";
import { useEffect, useState } from "react";

const REPO = "cubeshipd/cubeship";
const CACHED = "cubeship.stars";

// How long a star count is believed. Stars move slowly and GitHub allows
// sixty unauthenticated calls an hour per address — a dashboard somebody
// leaves open would otherwise spend them on a number nobody is watching.
const TTL = 6 * 60 * 60 * 1000;

// GitHubLink is the one link out of the instance.
//
// **The count is fetched by the browser, not by the daemon**, and that is
// the right way round here: an instance may be on a box with no route to
// the internet, and the person looking at it is on a laptop that has
// one. It also keeps a third-party call out of the daemon, which is
// otherwise reachable-from-nothing by design.
//
// Failing means no number. The link is the point; the count is
// decoration, and decoration that turns into an error message is worse
// than decoration that is absent.
export function GitHubLink() {
  const [stars, setStars] = useState<number | null>(null);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(CACHED);
      if (raw) {
        const { at, count } = JSON.parse(raw) as { at: number; count: number };
        if (Date.now() - at < TTL) {
          setStars(count);
          return;
        }
      }
    } catch {
      // A browser with storage turned off asks every time, which is
      // fine — it is one request per load, not per render.
    }

    fetch(`https://api.github.com/repos/${REPO}`, {
      headers: { Accept: "application/vnd.github+json" },
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((body: { stargazers_count?: number } | null) => {
        const count = body?.stargazers_count;
        if (typeof count !== "number") return;
        setStars(count);
        try {
          localStorage.setItem(CACHED, JSON.stringify({ at: Date.now(), count }));
        } catch {}
      })
      .catch(() => {});
  }, []);

  return (
    <a
      href={`https://github.com/${REPO}`}
      target="_blank"
      rel="noreferrer"
      title="Cubeship on GitHub"
      className="flex h-8 shrink-0 items-center gap-1.5 border border-transparent px-2 text-subtle-foreground transition-colors hover:border-border hover:bg-secondary hover:text-foreground"
    >
      <SiGithub className="size-4" />
      {stars !== null && <span className="font-mono text-[11px]">{abbreviate(stars)}</span>}
      <span className="sr-only">Cubeship on GitHub</span>
    </a>
  );
}

// abbreviate keeps the number to three characters and a suffix, which is
// what fits beside an icon in a sidebar: 1200 is 1.2k, 12000 is 12k.
//
// The decimal is dropped once the integer part has two digits, because
// "12.3k" is precision nobody reads at this size and the width is what
// is actually scarce here.
function abbreviate(n: number): string {
  for (const [limit, suffix] of [
    [1_000_000_000, "b"],
    [1_000_000, "m"],
    [1_000, "k"],
  ] as const) {
    if (n >= limit) {
      const scaled = n / limit;
      return scaled >= 10 ? `${Math.round(scaled)}${suffix}` : `${scaled.toFixed(1)}${suffix}`;
    }
  }
  return String(n);
}
