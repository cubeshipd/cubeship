"use client";

import { CornerDownLeftIcon, SearchIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import {
  type App,
  api,
  type Datastore,
  type DNSProvider,
  type ObjectStore,
  type Project,
  type RegistryCredential,
} from "@/lib/api";

// Two things behind one box.
//
// **Cmd+Shift+P is what to do; Cmd+K is what to open.** They are
// different questions and the answers do not belong in one list:
// "New database" and the database called `pg` would sit beside each
// other under `d`, and picking the wrong one is either a form you did
// not want or a screen you did not want. VS Code splits them the same
// way and for the same reason; `>` at the front of the query switches
// to commands, which is the convention and costs one character.
//
// **What a command does not contain is anything irreversible.** Every
// one of them opens a form. Nothing here deploys, deletes or
// provisions: a fuzzy search with an irreversible act at the end of it
// is a way to press the wrong button quickly, and everything
// irreversible in this product is deliberately behind a screen you went
// to and a word you typed.
//
// The instance is small — one VPS, a handful of each thing — so search
// fetches the whole catalogue when it opens and matches it in memory.
// There is no endpoint for this and there does not need to be one.

const NAV_KIND = "screen";

// What a command does. It is a link, always: every create form on this
// instance lives on the screen its result lands on, holds that screen's
// state and closes back onto that screen's list — so the command goes
// there and asks it to open, through `useOpenOnArrival`. The palette
// owning a second copy of seven forms would be seven things to keep in
// step for no gain.
const COMMANDS: Entry[] = [
  { id: "c:project", label: "New project", kind: "command", href: "/projects?new=1" },
  { id: "c:database", label: "New database", kind: "command", href: "/databases?new=1" },
  { id: "c:store", label: "Link object storage", kind: "command", href: "/storage?new=1" },
  { id: "c:registry", label: "Connect a registry", kind: "command", href: "/registries?new=1" },
  { id: "c:dns", label: "Connect a DNS provider", kind: "command", href: "/dns?new=1" },
  { id: "c:credential", label: "New credential", kind: "command", href: "/credentials?new=1" },
  { id: "c:server", label: "Add a server", kind: "command", href: "/servers?new=1" },
  // No "New app": an app is created inside an environment, and from
  // here there is no environment to create it in. Offering it would
  // mean picking one on somebody's behalf or landing them on a screen
  // to choose — which is the projects grid, one keystroke away in the
  // other half of this box.
  { id: "c:account", label: "Add someone to this instance", kind: "command", href: "/users" },
];

type Entry = {
  id: string;
  label: string;
  detail?: string;
  kind: string;
  href: string;
  // What the query is matched against, where that is not what is
  // shown.
  //
  // An app is the case it exists for: it is *shown* as `api` beside
  // `web/production`, because that is how it reads in a list — and it
  // is *matched* on `web/production/api`, because `wpa` is what
  // somebody types and the letters have to be in one string and in that
  // order for a subsequence to find them.
  match?: string;
};

type Mode = "command" | "search";

export function CommandPalette({ screens }: { screens: { label: string; href: string }[] }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<Mode>("search");
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState(0);
  const [resources, setResources] = useState<Entry[]>([]);
  const list = useRef<HTMLDivElement>(null);
  // The key handler is bound once, so it reads the mode through a ref
  // rather than closing over a value from the render it was bound in.
  const modeRef = useRef<Mode>(mode);
  modeRef.current = mode;

  // **`code`, not `key`.** With Shift held, `key` is the shifted
  // character, and on a layout where P is somewhere else it is not P at
  // all. `code` is the physical key, which is what a shortcut means.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod) return;
      // Cmd+K as well as the one that was asked for: it is what hands
      // reach for, it is free, and a palette that ignores it reads as a
      // palette that is broken.
      const asked: Mode | null =
        e.code === "KeyP" && e.shiftKey
          ? "command"
          : e.code === "KeyK" && !e.shiftKey
            ? "search"
            : null;
      if (asked === null) return;
      e.preventDefault();
      // The same chord closes it; the other one switches to that half
      // rather than shutting the box you are looking at.
      setOpen((was) => (was && asked === modeRef.current ? false : true));
      setMode(asked);
      setQuery("");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // `>` at the front reaches the other half without the chord, which is
  // what every palette with two halves does.
  const typed = query.startsWith(">") ? query.slice(1) : query;
  const showing: Mode = query.startsWith(">") ? "command" : mode;

  // Loaded when it opens and not before. The screens are there from the
  // first frame either way, so the palette is never an empty box while
  // six requests land.
  useEffect(() => {
    if (!open || showing !== "search") return;
    let live = true;
    catalogue().then((found) => {
      if (live) setResources(found);
    });
    return () => {
      live = false;
    };
  }, [open, showing]);

  // A new query, or the other half, starts at the top.
  useEffect(() => {
    setPicked(0);
  }, []);

  const entries = useMemo<Entry[]>(
    () =>
      showing === "command"
        ? COMMANDS
        : [
            ...screens.map((s) => ({
              id: `nav:${s.href}`,
              label: s.label,
              kind: NAV_KIND,
              href: s.href,
            })),
            ...resources,
          ],
    [showing, screens, resources],
  );

  const results = useMemo(() => {
    const q = typed.trim();
    if (q === "") return entries.slice(0, 12);
    return entries
      .map((e) => ({ e, score: match(e.match ?? `${e.label} ${e.detail ?? ""}`, q) }))
      .filter((r): r is { e: Entry; score: number } => r.score !== null)
      .sort((a, b) => b.score - a.score)
      .slice(0, 12)
      .map((r) => r.e);
  }, [entries, typed]);

  const go = useCallback(
    (entry: Entry | undefined) => {
      if (!entry) return;
      setOpen(false);
      router.push(entry.href);
    },
    [router],
  );

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setPicked((i) => {
        const next = e.key === "ArrowDown" ? i + 1 : i - 1;
        // Wrapping, because a list of twelve reached from a keyboard is
        // faster to come back round than to arrow all the way up.
        return (next + results.length) % Math.max(results.length, 1);
      });
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      go(results[picked]);
    }
  };

  // Keep the highlighted row in view when the arrows walk past the
  // fold. By index rather than by the attribute that marks it: the
  // index is what changed, and an effect that reads the DOM instead is
  // one whose dependency list cannot be checked.
  useEffect(() => {
    list.current?.querySelectorAll("button")[picked]?.scrollIntoView({ block: "nearest" });
  }, [picked]);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="top-[18%] translate-y-0 gap-0 p-0 sm:max-w-xl"
        showCloseButton={false}
        aria-label="Go to"
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <SearchIcon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          {/* autoFocus: a palette that does not take the caret is one
              you have to click, which is the thing it exists to save. */}
          <input
            autoFocus
            value={query}
            spellCheck={false}
            placeholder={showing === "command" ? "Run a command…" : "Go to…"}
            onChange={(e) => {
              setQuery(e.target.value);
              setPicked(0);
            }}
            onKeyDown={onKeyDown}
            className="h-11 min-w-0 flex-1 bg-transparent font-mono text-sm outline-none placeholder:font-sans placeholder:text-muted-foreground"
          />
        </div>

        <div ref={list} className="max-h-80 overflow-y-auto p-1">
          {results.length === 0 && (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">
              Nothing matches that.
            </p>
          )}
          {results.map((entry, i) => (
            <button
              key={entry.id}
              type="button"
              data-picked={i === picked}
              onMouseMove={() => setPicked(i)}
              onClick={() => go(entry)}
              className="flex w-full items-center gap-3 px-3 py-2 text-left transition-colors data-[picked=true]:bg-secondary"
            >
              <span className="min-w-0 flex-1 truncate font-mono text-sm">{entry.label}</span>
              {entry.detail && (
                <span className="shrink-0 truncate font-mono text-[11px] text-muted-foreground">
                  {entry.detail}
                </span>
              )}
              <span className="shrink-0 text-[10px] tracking-[0.14em] text-subtle-foreground uppercase">
                {entry.kind}
              </span>
              {i === picked && (
                <CornerDownLeftIcon aria-hidden="true" className="size-3 shrink-0 text-primary" />
              )}
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// Everything on the instance worth going to, in parallel.
//
// A failure is an absence rather than an error: the palette is a way to
// get somewhere faster, and one that refuses to open because the DNS
// list timed out is slower than the sidebar.
async function catalogue(): Promise<Entry[]> {
  const [projects, apps, datastores, stores, registries, dns] = await Promise.all([
    give<Project[]>("/projects"),
    give<App[]>("/apps"),
    give<Datastore[]>("/datastores"),
    give<ObjectStore[]>("/objectstores"),
    give<RegistryCredential[]>("/registries"),
    give<DNSProvider[]>("/dns"),
  ]);

  return [
    ...apps.map((a) => ({
      id: `app:${a.reference}`,
      label: a.name,
      detail: `${a.project}/${a.environment}`,
      match: a.reference,
      kind: "app",
      href: `/projects/${a.reference}`,
    })),
    ...projects.map((p) => ({
      id: `project:${p.slug}`,
      label: p.slug,
      kind: "project",
      href: `/projects/${p.slug}`,
    })),
    ...datastores.map((d) => ({
      id: `db:${d.name}`,
      label: d.name,
      detail: `${d.engine} ${d.version}`,
      kind: "database",
      href: `/databases/${d.name}`,
    })),
    ...stores.map((s) => ({
      id: `store:${s.name}`,
      label: s.name,
      detail: s.provider,
      kind: "store",
      href: `/storage/${s.name}`,
    })),
    ...registries.map((r) => ({
      id: `registry:${r.id}`,
      label: r.host,
      kind: "registry",
      href: `/registries/${r.id}`,
    })),
    ...dns.map((p) => ({
      id: `dns:${p.id}`,
      label: p.provider_name,
      detail: p.label,
      kind: "dns",
      href: `/dns/${p.id}`,
    })),
  ];
}

async function give<T>(path: string): Promise<T extends unknown[] ? T : never> {
  try {
    return (await api.get<T>(path)) as T extends unknown[] ? T : never;
  } catch {
    return [] as unknown as T extends unknown[] ? T : never;
  }
}

// match scores a subsequence, or answers null when there is not one.
//
// **Subsequence rather than substring**, which is the whole difference
// between a palette and a filter: `wpa` should find
// `web/production/api`, and nobody types the slashes. The score is what
// puts the answer somebody meant at the top — a run of letters together
// is worth more than the same letters scattered, and a letter after a
// separator is worth more than one in the middle of a word, because
// that is where people abbreviate from.
function match(text: string, query: string): number | null {
  const haystack = text.toLowerCase();
  const needle = query.toLowerCase().replace(/\s+/g, "");
  if (needle === "") return 0;

  let score = 0;
  let at = 0;
  let run = 0;
  for (const ch of needle) {
    const found = haystack.indexOf(ch, at);
    if (found === -1) return null;
    const boundary = found === 0 || /[\s/\-_.:]/.test(haystack[found - 1]);
    run = found === at ? run + 1 : 0;
    score += 1 + run * 4 + (boundary ? 6 : 0);
    at = found + 1;
  }
  // A short name that matched is a better answer than a long one that
  // matched the same letters: `api` should beat `api-gateway-internal`.
  return score - haystack.length * 0.05;
}
