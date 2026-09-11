"use client";

import { useEffect } from "react";

// Opens a screen's own dialog because the link that brought you here
// asked for it.
//
// **It is how the command palette runs a command.** "New database" is a
// form that lives on the databases screen, holds that screen's state and
// closes back onto that screen's list — the palette has no business
// owning a second copy of it, and lifting every create form into the
// Shell to make one reachable would be a large change to make a small
// one. So the command navigates with `?new=1` and the screen opens the
// thing it already has.
//
// The parameter is **removed as soon as it is read**, with `replace` so
// it leaves no history entry. Left in the URL it would be a link that
// reopens the dialog on every visit, and the first thing anybody does
// with a URL is bookmark it or send it to somebody.
export function useOpenOnArrival(flag: string, open: (open: true) => void) {
  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get(flag) === null) return;
    url.searchParams.delete(flag);
    window.history.replaceState(null, "", url.pathname + url.search + url.hash);
    open(true);
    // It takes `true` so a `useState` setter can be passed straight in,
    // which is what every caller has: the alternative is seven arrow
    // functions that all say the same thing.
  }, [flag, open]);
}
