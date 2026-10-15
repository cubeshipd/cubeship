"use client";

import { createContext, type ReactNode, useContext } from "react";
import type { Me } from "@/lib/api";

// Who is signed in. The shell has already resolved it — it will not
// render anything until it has — so everything below can read it
// without asking again.
const Context = createContext<Me | null>(null);

// How a screen that changes the account tells the shell. Without it the
// copy resolved at load goes stale: a theme chosen on Appearance was put
// back to the old one the next time the tab mounted, because the tab
// re-applied what the stale copy said.
const Update = createContext<(change: Partial<Me>) => void>(() => {});

export function SessionProvider({
  me,
  update,
  children,
}: {
  me: Me;
  update: (change: Partial<Me>) => void;
  children: ReactNode;
}) {
  return (
    <Context.Provider value={me}>
      <Update.Provider value={update}>{children}</Update.Provider>
    </Context.Provider>
  );
}

// useSession is safe anywhere under the shell, which is everywhere a
// signed-in page renders.
export function useSession(): Me {
  const me = useContext(Context);
  if (!me) {
    throw new Error("useSession outside the shell");
  }
  return me;
}

/** useUpdateSession merges a change into the signed-in account. */
export function useUpdateSession() {
  return useContext(Update);
}
