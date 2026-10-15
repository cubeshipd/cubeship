"use client";

import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { useUpdateSession } from "@/components/session-context";
import { api, type Me } from "@/lib/api";
import { message } from "@/lib/errors";

// Where the browser keeps its copy of the chosen palette.
//
// **The row on the account is what is true**, and this is what makes
// the first paint right: the daemon has not been asked yet when the
// page renders, and a dashboard that renders in one set of colours and
// swaps to another a moment later is worse than one that only ever had
// the default.
const CACHED =
  process.env.NEXT_PUBLIC_CUBESHIP_DEMO === "1" ? "cubeship.demo.theme" : "cubeship.theme";

/** applyTheme paints the interface in one of the palettes. */
export function applyTheme(theme: string | undefined) {
  const root = document.documentElement;
  if (theme) {
    root.setAttribute("data-theme", theme);
  } else {
    root.removeAttribute("data-theme");
  }
  try {
    if (theme) localStorage.setItem(CACHED, theme);
    else localStorage.removeItem(CACHED);
  } catch {
    // Storage turned off: the theme still applies, it just costs a
    // flash on the next load.
  }
}

/**
 * useTheme is the caller's palette, and how to change it.
 *
 * Saved to the account, because it is a fact about the person rather
 * than about the machine they opened this on.
 */
export function useTheme(me: Me) {
  const [theme, setTheme] = useState(me.theme ?? "");
  const [busy, setBusy] = useState(false);
  const update = useUpdateSession();

  // What the daemon says wins over what the browser remembered — a
  // preference changed on another machine has to arrive here.
  useEffect(() => {
    applyTheme(me.theme);
    setTheme(me.theme ?? "");
  }, [me.theme]);

  const choose = useCallback(
    async (next: string) => {
      if (busy) return;
      const previous = theme;
      // Painted first, then saved. It is the one setting where the
      // result is the feedback, and a spinner between the click and the
      // colour would be the whole interaction.
      applyTheme(next);
      setTheme(next);
      // The session too, or the effect above puts the old one back the
      // next time this mounts.
      update({ theme: next });
      setBusy(true);
      try {
        await api.patch<Me>("/users/me", { theme: next });
      } catch (error) {
        applyTheme(previous);
        setTheme(previous);
        update({ theme: previous });
        toast.error(message(error));
      } finally {
        setBusy(false);
      }
    },
    [update, theme, busy],
  );

  return { theme, choose, busy };
}

/**
 * ThemeBoot paints the remembered palette before anything is fetched.
 *
 * It runs as an inline script in the document head rather than as an
 * effect, because an effect runs after the first paint — which is the
 * flash it exists to prevent.
 */
export function ThemeBoot() {
  return (
    <script
      // biome-ignore lint/security/noDangerouslySetInnerHtml: a constant string, no interpolation
      dangerouslySetInnerHTML={{
        __html: `try{var t=localStorage.getItem(${JSON.stringify(CACHED)});if(t)document.documentElement.setAttribute("data-theme",t)}catch(e){}`,
      }}
    />
  );
}
