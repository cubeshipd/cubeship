"use client";

import { ChevronDownIcon, type BoxIcon as MarkIcon } from "lucide-react";
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
import { MARKS as SHARED } from "@/components/marks";
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
  type DNSProvider,
  type DNSZone,
  type Environment,
  type ObjectStore,
  type Project,
  type RegistryCredential,
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

type RailContext = { slot: HTMLElement | null };
const Rail = createContext<RailContext>({ slot: null });

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

export function HeaderRail({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const pathname = usePathname() ?? "/";
  const crumbs = crumbsFor(pathname);

  const value = useMemo(() => ({ slot }), [slot]);

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
                <Crumb crumb={crumb} last={i === crumbs.length - 1} />
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
type Siblings =
  | "project"
  | "environment"
  | "app"
  | "datastore"
  | "objectstore"
  | "bucket"
  | "registry"
  | "dnsprovider"
  | "zone";

// An option in a crumb's menu: what it is called, and what goes in the
// path.
//
// **They are not always the same.** A project, an app and a bucket are
// addressed by their own name, so the two are one string — but a
// registry and a DNS provider are addressed by a credential's numeric
// id, and a menu offering `4` and `7` is a menu nobody can read.
type Option = { label: string; value: string };

// The mark a menu's rows wear, from the one map the rail and the
// palette share — see components/marks.tsx. On the options rather than
// on the crumb: the path is a line of words and stays one, and what an
// icon per row buys is an edge to read down.
const MARKS: Record<Siblings, typeof MarkIcon> = {
  project: SHARED.project,
  environment: SHARED.environment,
  app: SHARED.app,
  datastore: SHARED.database,
  objectstore: SHARED.store,
  bucket: SHARED.bucket,
  registry: SHARED.registry,
  dnsprovider: SHARED.dns,
  zone: SHARED.zone,
};

// The kinds whose path segment is an id rather than a name, so the
// crumb has to be told what it is called before it can say anything.
//
// It is the one case that loads without being opened: everywhere else
// the crumb reads its own label straight off the URL and the list is
// only wanted once somebody asks for it.
const NAMED_BY_ID = new Set<Siblings>(["registry", "dnsprovider"]);

// What the thing under a section is, where it is something with peers.
// Credentials, users and the rest are lists with no page under them, so
// they are absent rather than mapped to nothing.
const SECTION_SIBLINGS: Record<string, Siblings | undefined> = {
  databases: "datastore",
  storage: "objectstore",
  registries: "registry",
  dns: "dnsprovider",
};

type CrumbSpec = {
  key: string;
  label: ReactNode;
  // Where clicking the crumb itself goes. Absent on the last one, and
  // on a word that names no page.
  href?: string;
  siblings?: Siblings;
  // What this crumb is, as the path spells it — which is the label
  // everywhere but the two kinds addressed by an id, where the label
  // has to be looked up and this is what to look it up by.
  value?: string;
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
  // The root has no segment to read, and `overview` is what the sidebar
  // calls it. Lowercase like every other crumb: it was the one word here
  // wearing a capital, which is exactly the kind of exception that reads
  // as a mistake rather than as a rule.
  if (parts.length === 0) return [{ key: "overview", label: "overview" }];

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
      value: project,
      href: env ? `/projects/${project}` : undefined,
      siblings: "project",
      scope: "",
    });
    if (env) {
      out.push({
        key: `e:${env}`,
        label: env,
        value: env,
        href: app ? `/projects/${project}/${env}` : undefined,
        siblings: "environment",
        scope: project,
      });
    }
    if (app) {
      out.push({
        key: `a:${app}`,
        label: app,
        value: app,
        href: tail.length > 0 ? `/projects/${project}/${env}/${app}` : undefined,
        siblings: "app",
        scope: `${project}/${env}`,
      });
    }
    for (const word of tail) out.push({ key: `t:${word}`, label: title(word) });
    return out;
  }

  const [name, ...tail] = rest;
  const siblings = SECTION_SIBLINGS[section];
  const out: CrumbSpec[] = [
    head,
    {
      key: `n:${name}`,
      label: name,
      value: name,
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
  // A zone under a DNS provider, which is the bucket's twin: a named
  // thing one level below a named thing, with peers worth switching to.
  //
  // It is addressed by its **name** rather than by the provider's id
  // for it — a name is what somebody recognises in a link they were
  // sent — so the label and the path are one string here.
  if (section === "dns" && tail[0] === "zones" && tail[1]) {
    out.push({ key: "t:zones", label: title("zones") });
    out.push({
      key: `z:${tail[1]}`,
      label: decodeURIComponent(tail[1]),
      value: decodeURIComponent(tail[1]),
      href: tail.length > 2 ? `/dns/${name}/zones/${tail[1]}` : undefined,
      siblings: "zone",
      scope: name,
    });
    for (const word of tail.slice(2)) out.push({ key: `t:${word}`, label: title(word) });
    return out;
  }

  if (section === "storage" && tail[0] === "buckets" && tail[1]) {
    out.push({ key: "t:buckets", label: title("buckets") });
    out.push({
      key: `b:${tail[1]}`,
      label: decodeURIComponent(tail[1]),
      value: decodeURIComponent(tail[1]),
      href: tail.length > 2 ? `/storage/${name}/buckets/${tail[1]}` : undefined,
      siblings: "bucket",
      scope: name,
    });
    for (const word of tail.slice(2)) out.push({ key: `t:${word}`, label: title(word) });
    return out;
  }

  // Everything else below one item — a record, a settings screen — is a
  // word rather than something with peers to offer.
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
// **The list is fetched when it is opened**, because the rail is on
// every screen and most of the time nobody touches it: loading every
// project, environment and app on every navigation would be a request
// per screen for a menu that stays shut.
//
// The exception is a crumb whose path segment is an id. It cannot say
// what it is called without the list, so there it loads on sight — and
// what the URL holds (`4`) is never what the crumb shows.
function CrumbMenu({ crumb, className }: { crumb: CrumbSpec; className: string }) {
  const router = useRouter();
  const [options, setOptions] = useState<Option[] | null>(null);
  const [open, setOpen] = useState(false);

  const scope = crumb.scope ?? "";
  const kind = crumb.siblings;
  const Mark = kind ? MARKS[kind] : null;
  const byID = kind !== undefined && NAMED_BY_ID.has(kind);

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
    if ((open || byID) && options === null) load();
  }, [open, byID, options, load]);

  // What the crumb is standing on, so the menu can mark it and — where
  // the path is an id — so the crumb has a word to show at all.
  const here = options?.find((o) => o.value === crumb.value);
  const label = byID ? (here?.label ?? "…") : crumb.label;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        className={`${className} flex min-w-0 items-center gap-1 truncate transition-colors hover:text-primary focus-visible:text-primary focus-visible:outline-none`}
      >
        <span className="truncate">{label}</span>
        <ChevronDownIcon aria-hidden="true" className="size-3 shrink-0 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 min-w-44 overflow-y-auto">
        {options === null && (
          <div className="px-2 py-1.5 font-mono text-[11px] text-muted-foreground">…</div>
        )}
        {options?.length === 0 && (
          <div className="px-2 py-1.5 text-[11px] text-muted-foreground">Nothing else here.</div>
        )}
        {options?.map((option) => (
          <DropdownMenuItem
            key={option.value}
            className="gap-2 font-mono text-[11px]"
            onClick={() => router.push(hrefFor(kind, scope, option.value))}
          >
            {Mark && <Mark aria-hidden="true" className="size-3.5 shrink-0 opacity-70" />}
            {option.label}
            {option.value === crumb.value && <span className="ml-auto text-primary">●</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

async function siblingsOf(kind: Siblings, scope: string): Promise<Option[]> {
  const [above, env] = scope.split("/");
  const same = (name: string): Option => ({ label: name, value: name });
  switch (kind) {
    case "project":
      return (await api.get<Project[]>("/projects")).map((p) => same(p.slug));
    case "environment":
      // `above`, not `scope[0]`: scope became a string when it had to be
      // a dependency, and indexing it took the first *character* — so
      // this asked a project called `w` for its environments and every
      // environment menu came back empty.
      return (await api.get<Environment[]>(`/projects/${above}/environments`)).map((e) =>
        same(e.slug),
      );
    case "app":
      return (await api.get<App[]>("/apps"))
        .filter((a) => a.project === above && a.environment === env)
        .map((a) => same(a.name));
    case "datastore":
      return (await api.get<Datastore[]>("/datastores")).map((d) => same(d.name));
    case "objectstore":
      return (await api.get<ObjectStore[]>("/objectstores")).map((s) => same(s.name));
    case "bucket":
      return (await api.get<Bucket[]>(`/objectstores/${above}/buckets`)).map((b) => same(b.name));
    case "registry": {
      // `GET /registries` is the logins for registries Cubeship does
      // **not** run, so its own is not in the answer — it has no
      // credential to be a row of. It is prepended here for the reason
      // the list screen puts it first: it is the one every instance
      // has, and `cubeship` is its reserved id.
      const linked = await api.get<RegistryCredential[]>("/registries");
      return [
        { label: "Cubeship registry", value: "cubeship" },
        ...linked.map((r) => ({ label: r.host, value: String(r.id) })),
      ];
    }
    case "dnsprovider":
      return (await api.get<DNSProvider[]>("/dns")).map((p) => ({
        label: p.provider_name,
        value: String(p.id),
      }));
    case "zone":
      return (await api.get<DNSZone[]>(`/dns/${above}/zones`)).map((z) => same(z.name));
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
    case "registry":
      return `/registries/${name}`;
    case "dnsprovider":
      return `/dns/${name}`;
    case "zone":
      return `/dns/${above}/zones/${encodeURIComponent(name)}`;
    default:
      return "/";
  }
}
