"use client";

import { SiGithub } from "@icons-pack/react-simple-icons";
import { useEffect, useState } from "react";
import { DropdownMenuItem, DropdownMenuShortcut } from "@/components/ui/dropdown-menu";

const REPO = "cubeshipd/cubeship";
const CACHED = "cubeship.stars";

// How long a star count is believed. Stars move slowly and GitHub allows
// sixty unauthenticated calls an hour per address — a dashboard somebody
// leaves open would otherwise spend them on a number nobody is watching.
const TTL = 6 * 60 * 60 * 1000;

// The one link out of the instance, and an ask.
//
// It sat in the sidebar's footer beside the account button, as an icon
// and a number: a link nobody reads as an invitation, taking width from
// the one control down there that people actually use. In the menu it
// can say what it wants — the count stops being decoration and becomes
// the reason the sentence works.
//
// **The count is fetched by the browser, not by the daemon**, and that
// is the right way round here: an instance may be on a box with no
// route to the internet, and the person looking at it is on a laptop
// that has one. It also keeps a third-party call out of the daemon,
// which is otherwise reachable-from-nothing by design.
//
// Failing means no number. The ask is the point; the count is what
// makes it concrete, and a number that turns into an error message is
// worse than one that is absent.
export function GitHubStar() {
  const stars = useStars();

  return (
    <DropdownMenuItem
      render={<a href={`https://github.com/${REPO}`} target="_blank" rel="noreferrer" />}
    >
      <SiGithub />
      Star on GitHub
      {stars !== null && (
        <DropdownMenuShortcut className="font-mono">{abbreviate(stars)}</DropdownMenuShortcut>
      )}
    </DropdownMenuItem>
  );
}

// useStars answers the count, from the cache when it is fresh.
//
// **Read before the first paint**, not in the effect: the menu's
// content is mounted when it opens and thrown away when it closes, so
// an effect-only read would make the number arrive a frame late on
// every open, on a value that was already known.
function useStars(): number | null {
  const [stars, setStars] = useState<number | null>(cached);

  useEffect(() => {
    if (cached() !== null) return;
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

  return stars;
}

function cached(): number | null {
  try {
    const raw = localStorage.getItem(CACHED);
    if (!raw) return null;
    const { at, count } = JSON.parse(raw) as { at: number; count: number };
    return Date.now() - at < TTL ? count : null;
  } catch {
    // A browser with storage turned off asks every time, which is fine:
    // it is one request per menu, not per render.
    return null;
  }
}

// abbreviate keeps the number to three characters and a suffix: 1200 is
// 1.2k, 12000 is 12k.
//
// The decimal is dropped once the integer part has two digits, because
// "12.3k" is precision nobody reads at this size.
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
