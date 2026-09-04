"use client";

import { createContext, type ReactNode, useContext } from "react";
import type { Me } from "@/lib/api";

// Who is signed in. The shell has already resolved it — it will not
// render anything until it has — so everything below can read it
// without asking again.
const Context = createContext<Me | null>(null);

export function SessionProvider({ me, children }: { me: Me; children: ReactNode }) {
  return <Context.Provider value={me}>{children}</Context.Provider>;
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
