"use client";

import { ChevronDownIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useState } from "react";
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
// It does not replace the page's own header. `PageHeader` keeps the
// title and the actions, and on a settings screen the two say different
// things: the path is the app, the title is "App settings".
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

  return (
    <Rail.Provider value={{ slot }}>
      {/* Sticky, because the reason it exists is to be reachable — and
          the screens where switching saves the most are the long ones:
          a log, fifty environment variables, a deploy history. */}
      <div className="sticky top-0 z-30 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-11 max-w-5xl items-center justify-between gap-4 px-8">
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
type Siblings = "project" | "environment" | "app" | "datastore" | "objectstore";

type CrumbSpec = {
  key: string;
  label: string;
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
  mono?: boolean;
};

// The first segment's name, which is the sidebar's word for it. Written
// out rather than derived, because "storage" is called "Object storage"
// and "settings" is called "Instance".
const sections: Record<string, string> = {
  projects: "Projects",
  databases: "Databases",
  storage: "Object storage",
  credentials: "Credentials",
  users: "Users",
  registries: "Registries",
  git: "Git Providers",
  dns: "DNS Providers",
  certificates: "Certificates",
  backups: "Backups",
  firewall: "Firewall",
  servers: "Servers",
  settings: "Instance",
  account: "Your settings",
};

export function crumbsFor(pathname: string): CrumbSpec[] {
  const parts = pathname.split("/").filter(Boolean);
  if (parts.length === 0) return [{ key: "overview", label: "Overview" }];

  const [section, ...rest] = parts;
  const head: CrumbSpec = {
    key: section,
    label: sections[section] ?? section,
    href: rest.length > 0 ? `/${section}` : undefined,
  };
  if (rest.length === 0) return [head];

  // The three-level hierarchy is the one worth switching inside, and
  // the only place a crumb has more than one kind of sibling.
  if (section === "projects") {
    const [project, env, app, ...tail] = rest;
    const out: CrumbSpec[] = [head];
    out.push({
      key: `p:${project}`,
      label: project,
      href: env ? `/projects/${project}` : undefined,
      siblings: "project",
      scope: "",
      mono: true,
    });
    if (env) {
      out.push({
        key: `e:${env}`,
        label: env,
        href: app ? `/projects/${project}/${env}` : undefined,
        siblings: "environment",
        scope: project,
        mono: true,
      });
    }
    if (app) {
      out.push({
        key: `a:${app}`,
        label: app,
        href: tail.length > 0 ? `/projects/${project}/${env}/${app}` : undefined,
        siblings: "app",
        scope: `${project}/${env}`,
        mono: true,
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
      mono: true,
    },
  ];
  // Everything below one item — a zone, a record, a settings screen —
  // is a word rather than something with peers to offer.
  for (const word of tail) out.push({ key: `t:${word}`, label: title(word) });
  return out;
}

function title(word: string): string {
  const known: Record<string, string> = { settings: "Settings", zones: "Zones" };
  return known[word] ?? decodeURIComponent(word);
}

function Crumb({ crumb, last }: { crumb: CrumbSpec; last: boolean }) {
  const text = crumb.mono ? "font-mono text-[11px]" : "text-[11px] tracking-wide uppercase";
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
            className="font-mono text-[11px]"
            onClick={() => router.push(hrefFor(kind, scope, name))}
          >
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
    default:
      return "/";
  }
}
