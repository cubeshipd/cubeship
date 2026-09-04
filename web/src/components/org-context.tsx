"use client";

import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { api, type Org } from "@/lib/api";

// Which organization you are looking at. It is not in the URL: a static
// export has no dynamic segments, and putting it in every query string
// would mean every link in the dashboard has to carry it. It is one
// choice a person makes rarely, so it lives here and is remembered.
const STORAGE_KEY = "cubeship.org";

type OrgState = {
  orgs: Org[];
  org: string;
  current: Org | undefined;
  select: (slug: string) => void;
  // Called after creating or deleting one, so the switcher and whatever
  // page is open see the same list.
  reload: () => Promise<Org[]>;
  loaded: boolean;
};

const Context = createContext<OrgState | null>(null);

export function OrgProvider({ children }: { children: ReactNode }) {
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [org, setOrg] = useState("");
  const [loaded, setLoaded] = useState(false);

  const settle = useCallback((list: Org[]) => {
    setOrgs(list);
    // A remembered organization that no longer exists — deleted, or
    // membership removed — must not leave the dashboard pointing at
    // nothing, so it falls back to the first one available.
    setOrg((current) => {
      const remembered = current || readStored();
      return list.some((o) => o.slug === remembered) ? remembered : (list[0]?.slug ?? "");
    });
    return list;
  }, []);

  const reload = useCallback(
    () =>
      api
        .get<Org[]>("/orgs")
        .then(settle)
        .catch(() => [] as Org[]),
    [settle],
  );

  useEffect(() => {
    reload().finally(() => setLoaded(true));
  }, [reload]);

  const select = useCallback((slug: string) => {
    setOrg(slug);
    try {
      localStorage.setItem(STORAGE_KEY, slug);
    } catch {
      // A browser that refuses storage still switches; it just forgets.
    }
  }, []);

  const value = useMemo<OrgState>(
    () => ({ orgs, org, current: orgs.find((o) => o.slug === org), select, reload, loaded }),
    [orgs, org, select, reload, loaded],
  );

  return <Context.Provider value={value}>{children}</Context.Provider>;
}

function readStored(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function useOrg(): OrgState {
  const ctx = useContext(Context);
  if (!ctx) throw new Error("useOrg must be used inside the Shell");
  return ctx;
}
