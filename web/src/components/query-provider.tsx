"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useState } from "react";

// The dashboard's data layer.
//
// Every page here was hand-rolling the same four lines — a null state, a
// useEffect that fetches, a setError, and a `load()` called again after
// every write — and getting a different one of them subtly wrong each
// time. The stale "Reading tags…" after a bulk delete and the double
// fetch of a zone listing were both that pattern, not bugs in the pages.
//
// The settings below are chosen for what this dashboard actually shows,
// which is somebody else's live state:
//
//   - **No retries on a 4xx.** A 401, a 403 and a 404 are answers, not
//     failures. Retrying one is three round trips to be told the same
//     thing, and for a provider probe it is three chances to be rate
//     limited.
//   - **Refetch on focus.** A registry or a DNS zone changes without
//     Cubeship being told; coming back to the tab is exactly when you
//     want what is on screen to be true.
//   - **A short staleTime rather than none.** Navigating between two
//     screens that read the same list should not ask twice, and ten
//     seconds is short enough that nothing looks frozen.
export function QueryProvider({ children }: { children: ReactNode }) {
  // Created in state, not at module scope: a client at module scope is
  // shared between every request the server renders, which would leak
  // one user's data into another's page.
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 10_000,
            refetchOnWindowFocus: true,
            retry: (failures, error) => {
              const status = (error as { status?: number })?.status;
              if (status !== undefined && status >= 400 && status < 500) return false;
              return failures < 2;
            },
          },
        },
      }),
  );

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
