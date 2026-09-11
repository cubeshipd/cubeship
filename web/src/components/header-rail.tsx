"use client";

import {
  BoxIcon,
  ChevronDownIcon,
  DatabaseIcon,
  FolderTreeIcon,
  HardDriveIcon,
  LayersIcon,
  PackageIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { createPortal } from "react-dom";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  type App,
  api,
  type Bucket,
  type Datastore,
  type Environment,
  type ObjectStore,
  type Project,
} from "@/lib/api";

// The strip above every screen: where you are, and what this screen
// puts there.
//
// **The path came out of the page.** Half the screens rendered their own
// "‹ back to the thing above" link and half rendered nothing, so the
// same question — where am I, and what is next to me — was answered
// differently on every one of them, or not at all. The Shell is the one
// place that can answer it once.
//
// **It is the page's header now.** There was a second one under it, a
// `PageHeader` per screen — and of the thirty-one titles it drew,
// almost every one was either the section the sidebar already
// highlights or the last word of this path. Two places saying it is one
// too many, so the last crumb wears the weight and the buttons that
// sat beside it come through `RailPortal`.
//
// **A crumb with siblings is a menu.** That is the whole point of
// putting it here rather than in a page: reading `web/production/api`
// tells you where you are, and being able to open `production` and land
// in `staging` is the trip back through two screens you no longer take.

type RailContext = {
  slot: HTMLElement | null;
  setTitle: (title: ReactNode | null) => void;
};
const Rail = createContext<RailContext>({ slot: null, setTitle: () => {} });

// RailPortal puts a screen's own controls in the rail, on the right.
//
// A portal rather than a prop threaded down from the layout: the thing
// that wants to put a control up there is usually several components
// deep — a tab's toolbar, a table's filter — and a prop would have to
// pass through every one of them to get there.
export function RailPortal({ children }: { children: ReactNode }) {
  const { slot } = useContext(Rail);
  if (!slot) return null;
  return createPortal(children, slot);
}

// RailTitle renames the last crumb, for a screen the URL cannot name.
//
// Most of them it can: an app, a database, a bucket and a zone are all
// addressed by the thing they are called. A registry and a DNS provider
// are addressed by a **credential's numeric id**, so the path segment
// is `4` — and a page whose title is `4` is a page with no title. It
// takes a node rather than a string so those two can keep the provider
// mark they had beside the name.
export function RailTitle({ children }: { children: ReactNode }) {
  const { setTitle } = useContext(Rail);
  useEffect(() => {
    setTitle(children);
  }, [children, setTitle]);
  return null;
}

export function HeaderRail({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [title, setTitle] = useState<ReactNode | null>(null);
  const pathname = usePathname() ?? "/";
  const crumbs = crumbsFor(pathname);

  // A title set by the page belongs to that page. Without clearing it
  // the next screen wears the last one's name for as long as its own
  // fetch takes — and the screens that set one are exactly the screens
  // that have to fetch to know it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: clearing is what changing page means
  useEffect(() => setTitle(null), [pathname]);

  const value = useMemo(() => ({ slot, setTitle }), [slot]);

  return (
    <Rail.Provider value={value}>
      {/* Sticky, because the reason it exists is to be reachable — and
          the screens where switching saves the most are the long ones:
          a log, fifty environment variables, a deploy history. */}
      {/* The height is on the element that carries the border, so the
          border is inside it — `h-14` is border-box. With the height on
          the inner div instead, this strip came to 57px against the
          sidebar header's 56, and the line across the top of the
          instance was two lines a pixel apart. */}
      <div className="sticky top-0 z-30 flex h-14 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-4 px-8">
          <nav
            aria-label="Breadcrumb"
            className="flex min-w-0 items-center gap-1.5 overflow-hidden"
          >
            {crumbs.map((crumb, i) => (
              <div key={crumb.key} className="flex min-w-0 items-center gap-1.5">
                {i > 0 && (
                  <span aria-hidden="true" className="text-subtle-foreground">
                    /
                  </span>
                )}
                <Crumb
                  crumb={
                    i === crumbs.length - 1 && title !== null ? { ...crumb, label: title } : crumb
                  }
                  last={i === crumbs.length - 1}
                />
              </div>
            ))}
          </nav>
          <div ref={setSlot} className="flex shrink-0 items-center gap-2" />
        </div>
      </div>
      {children}
    </Rail.Provider>
  );
}

// --- what a crumb is ---

// siblings says what a crumb can be swapped for. Absent is a crumb that
// names something with no peers worth offering — a section, a settings
// screen.
type Siblings = "project" | "environment" | "app" | "datastore" | "objectstore" | "bucket";

// A mark for each kind, worn by the rows inside the menu.
//
// **On the options, not on the crumb.** The path is a line of words and
// wants to stay one; what the mark does is give the menu that opens out
// of it an edge to read down, so a list of slugs reads as a list of
// *buckets* rather than as four bare words floating under a chevron.
//
// Projects, databases and stores wear the sidebar's own icon, because
// they are the same things it lists. The three that are not in the
// sidebar are chosen to sit apart from it: an environment is layers, an
// app is a box, a bucket is a package — none of them the container the
// registries entry already owns.
const MARKS: Record<Siblings, typeof BoxIcon> = {
  project: FolderTreeIcon,
  environment: LayersIcon,
  app: BoxIcon,
  datastore: DatabaseIcon,
  objectstore: HardDriveIcon,
  bucket: PackageIcon,
};

type CrumbSpec = {
  key: string;
  label: ReactNode;
  // Where clicking the crumb itself goes. Absent on the last one, and
  // on a word that names no page.
  href?: string;
  siblings?: Siblings;
  // The path above this crumb, which a sibling lookup needs: an app's
  // peers are the apps in *this* environment.
  //
  // A joined string rather than an array, so it can be a dependency:
  // `crumbsFor` runs on every render, and an array would be a new one
  // each time — a callback that is never the same twice.
  scope?: string;
};

// The segments under /projects that name a screen rather than a
// resource. `slug.Reserved` on the daemon refuses these as names for
// the same reason, so the two lists say the same thing from opposite
// ends: nothing here can be called `settings`, and nothing called
// `settings` here is a thing.
const STATIC_SEGMENTS = new Set(["settings"]);

// The path as the URL writes it, and the URL is already the reference.
//
// There was a table here turning `storage` into "Object storage" and
// `settings` into "Instance" — the sidebar's words, kept in step with
// the sidebar by hand. What it bought was a nicer noun in one place and
// a second list to forget to update; what it cost was a crumb that did
// not match the address it names.

export function crumbsFor(pathname: string): CrumbSpec[] {
  const parts = pathname.split("/").filter(Boolean);
  if (parts.length === 0) return [{ key: "overview", label: "Overview" }];

  const [section, ...rest] = parts;
  const head: CrumbSpec = {
    key: section,
    label: section,
    href: rest.length > 0 ? `/${section}` : undefined,
  };
  if (rest.length === 0) return [head];

  // The three-level hierarchy is the one worth switching inside, and
  // the only place a crumb has more than one kind of sibling.
  if (section === "projects") {
    // **A trailing `settings` is a screen, not a slug.** Next resolves
    // a static segment before a dynamic one, which is exactly why
    // `slug.Reserved` refuses the name at creation — and reading the
    // path positionally makes `/projects/web/settings` a project called
    // `web` in an *environment* called `settings`, which then offers a
    // menu of the environments it might be swapped for. It was one, and
    // it said "Nothing else here."
    const words = rest.slice();
    const trailing: string[] = [];
    while (words.length > 0 && STATIC_SEGMENTS.has(words[words.length - 1])) {
      trailing.unshift(words.pop() as string);
    }
    const [project, env, app] = words;
    const tail = trailing;
    const out: CrumbSpec[] = [head];
    out.push({
      key: `p:${project}`,
      label: project,
      href: env ? `/projects/${project}` : undefined,
      siblings: "project",
      scope: "",
    });
    if (env) {
      out.push({
        key: `e:${env}`,
        label: env,
        href: app ? `/projects/${project}/${env}` : undefined,
        siblings: "environment",
        scope: project,
      });
    }
    if (app) {
      out.push({
        key: `a:${app}`,
        label: app,
        href: tail.length > 0 ? `/projects/${project}/${env}/${app}` : undefined,
        siblings: "app",
        scope: `${project}/${env}`,
      });
    }
    for (const word of tail) out.push({ key: `t:${word}`, label: title(word) });
    return out;
  }

  const [name, ...tail] = rest;
  const siblings =
    section === "databases" ? "datastore" : section === "storage" ? "objectstore" : undefined;
  const out: CrumbSpec[] = [
    head,
    {
      key: `n:${name}`,
      label: name,
      href: tail.length > 0 ? `/${section}/${name}` : undefined,
      siblings,
      scope: "",
    },
  ];

  // A bucket is the one thing below a named item that has peers, and it
  // gets a menu **whether or not there is a second one**. The control
  // being in the same place every time is what makes it a control; one
  // that appears only once a store has two buckets is one nobody learns
  // is there, and the count is not something you know before you look.
  if (section === "storage" && tail[0] === "buckets" && tail[1]) {
    out.push({ key: "t:buckets", label: title("buckets") });
    out.push({
      key: `b:${tail[1]}`,
      label: decodeURIComponent(tail[1]),
      href: tail.length > 2 ? `/storage/${name}/buckets/${tail[1]}` : undefined,
      siblings: "bucket",
      scope: name,
    });
    for (const word of tail.slice(2)) out.push({ key: `t:${word}`, label: title(word) });
    return out;
  }

  // Everything else below one item — a zone, a record, a settings
  // screen — is a word rather than something with peers to offer.
  for (const word of tail) out.push({ key: `t:${word}`, label: title(word) });
  return out;
}

// Every segment reads as the URL writes it — see the crumb's own note.
// Capitalising `settings` into "Settings" was the last of the two lists
// that had to agree with each other.
function title(word: string): string {
  return decodeURIComponent(word);
}

// One crumb, and every crumb looks like it.
//
// It did not, for a day: the section was uppercase, the slugs were mono
// lowercase, and the last one was half again as large because it was
// standing in for a page title. Three typographic systems in one line,
// which reads as three different kinds of thing rather than as one
// path — and the largest of them was often the least informative word
// on the screen.
//
// So: mono throughout, one size, written the way the URL writes it.
// The only thing that separates the last crumb is that it is lit and
// the rest are not, which is what a breadcrumb has always done, and it
// is enough to say which of them you are standing on.
function Crumb({ crumb, last }: { crumb: CrumbSpec; last: boolean }) {
  const text = "font-mono text-sm";
  const tone = last ? "text-foreground" : "text-muted-foreground";

  if (crumb.siblings) {
    return <CrumbMenu crumb={crumb} className={`${text} ${tone}`} />;
  }
  if (crumb.href) {
    return (
      <Link
        href={crumb.href}
        className={`${text} ${tone} truncate transition-colors hover:text-primary`}
      >
        {crumb.label}
      </Link>
    );
  }
  return <span className={`${text} ${tone} truncate`}>{crumb.label}</span>;
}

// CrumbMenu is a crumb you can open.
//
// **The list is fetched when it is opened, never before.** The rail is
// on every screen and most of the time nobody touches it, so loading
// every project, environment and app on every navigation would be a
// request per screen for a menu that stays shut.
function CrumbMenu({ crumb, className }: { crumb: CrumbSpec; className: string }) {
  const router = useRouter();
  const [options, setOptions] = useState<string[] | null>(null);
  const [open, setOpen] = useState(false);

  const scope = crumb.scope ?? "";
  const kind = crumb.siblings;
  const Mark = kind ? MARKS[kind] : null;

  const load = useCallback(() => {
    if (!kind) return;
    siblingsOf(kind, scope)
      .then(setOptions)
      // A menu that cannot be filled is a menu with one entry: the
      // thing you are already looking at. Nothing is said about it —
      // the crumb still reads, which is its first job.
      .catch(() => setOptions([]));
  }, [kind, scope]);

  useEffect(() => {
    if (open && options === null) load();
  }, [open, options, load]);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        className={`${className} flex min-w-0 items-center gap-1 truncate transition-colors hover:text-primary focus-visible:text-primary focus-visible:outline-none`}
      >
        <span className="truncate">{crumb.label}</span>
        <ChevronDownIcon aria-hidden="true" className="size-3 shrink-0 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 min-w-44 overflow-y-auto">
        {options === null && (
          <div className="px-2 py-1.5 font-mono text-[11px] text-muted-foreground">…</div>
        )}
        {options?.length === 0 && (
          <div className="px-2 py-1.5 text-[11px] text-muted-foreground">Nothing else here.</div>
        )}
        {options?.map((name) => (
          <DropdownMenuItem
            key={name}
            className="gap-2 font-mono text-[11px]"
            onClick={() => router.push(hrefFor(kind, scope, name))}
          >
            {Mark && <Mark aria-hidden="true" className="size-3.5 shrink-0 opacity-70" />}
            {name}
            {name === crumb.label && <span className="ml-auto text-primary">●</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

async function siblingsOf(kind: Siblings, scope: string): Promise<string[]> {
  const [above, env] = scope.split("/");
  switch (kind) {
    case "project":
      return (await api.get<Project[]>("/projects")).map((p) => p.slug);
    case "environment":
      return (await api.get<Environment[]>(`/projects/${scope[0]}/environments`)).map(
        (e) => e.slug,
      );
    case "app":
      return (await api.get<App[]>("/apps"))
        .filter((a) => a.project === above && a.environment === env)
        .map((a) => a.name);
    case "datastore":
      return (await api.get<Datastore[]>("/datastores")).map((d) => d.name);
    case "objectstore":
      return (await api.get<ObjectStore[]>("/objectstores")).map((s) => s.name);
    case "bucket":
      return (await api.get<Bucket[]>(`/objectstores/${above}/buckets`)).map((b) => b.name);
  }
}

// **Switching lands on that level, never on the deep path you were on.**
// Picking another project while looking at an app does not go looking
// for an app of the same name in it — that app may not exist, and a
// menu that sometimes 404s is a menu nobody trusts twice. An app's
// peers are the only list already scoped to where you are, so that one
// stays where it is.
function hrefFor(kind: Siblings | undefined, scope: string, name: string): string {
  const [above, env] = scope.split("/");
  switch (kind) {
    case "project":
      return `/projects/${name}`;
    case "environment":
      return `/projects/${above}/${name}`;
    case "app":
      return `/projects/${above}/${env}/${name}`;
    case "datastore":
      return `/databases/${name}`;
    case "objectstore":
      return `/storage/${name}`;
    case "bucket":
      return `/storage/${above}/buckets/${encodeURIComponent(name)}`;
    default:
      return "/";
  }
}
