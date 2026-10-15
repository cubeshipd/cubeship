"use client";

import {
  ActivityIcon,
  ArchiveIcon,
  ChevronsUpDownIcon,
  ContainerIcon,
  DatabaseIcon,
  FolderTreeIcon,
  GitBranchIcon,
  GlobeIcon,
  HardDriveIcon,
  KeyRoundIcon,
  LayoutTemplateIcon,
  LogOutIcon,
  MenuIcon,
  ScrollTextIcon,
  ServerCogIcon,
  ServerIcon,
  SettingsIcon,
  ShieldCheckIcon,
  ShieldIcon,
  SparklesIcon,
  UsersIcon,
} from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Wordmark } from "@/components/brand";
import { CommandPalette } from "@/components/command-palette";
import { GitHubStar } from "@/components/github-star";
import { HeaderRail } from "@/components/header-rail";
import { InstanceUpdate } from "@/components/instance-update";
import Link from "@/components/navigation-link";
import { SessionLoading } from "@/components/page-loading";
import { QueryProvider } from "@/components/query-provider";
import { ReleaseNotes, useReleaseNotes } from "@/components/release-notes";
import { SessionProvider } from "@/components/session-context";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { UserAvatar } from "@/components/user-avatar";
import { api, can, type Me, personName } from "@/lib/api";
import { message } from "@/lib/errors";

// The two layers, and what separates them.
//
// **Workspace** is what you deploy: an organization's projects, the
// environments in them, the apps in those. It is the work.
//
// **Platform** is what the instance is wired to — the registries it can
// pull from, the DNS accounts it can write to, and the instance's own
// domain and credentials. Nothing in it belongs to a project, and
// almost none of it is touched twice: you connect a registry once and
// then deploy through it for a year.
//
// Flat, those two read as one list of five peers, and "Registries" sat
// beside "Projects" as though choosing between them were a normal thing
// to do. They are not peers: one is where you work and the other is the
// wiring behind it.
//
// `owns` is the rest of a section: the pages you reach from an entry
// that have no entry of their own. Projects is a section rather than a
// page — a project, an environment and an app all live under it, and
// all four keep it lit. Adding a page under one is an edit here, not a
// new special case in the component below.
export const sections: { label?: string; items: NavItem[] }[] = [
  {
    items: [
      // The address you land on, and the one thing on this instance
      // that is about the instance rather than about something in it:
      // what the machine is doing, and what is deployed on it. It is
      // above Projects because it is what you want before you know
      // which project you want.
      { href: "/", label: "Overview", icon: ActivityIcon },
      {
        href: "/projects",
        label: "Projects",
        icon: FolderTreeIcon,
        owns: ["/projects", "/environments", "/apps"],
        resource: ["projects", "apps"],
      },
      // Beside Projects rather than under Platform: a database belongs
      // to the instance, but it is a thing you deploy against, not a
      // thing the instance is wired to. It is opened as often as an app
      // is, which is what the Platform section is not for.
      {
        href: "/databases",
        label: "Databases",
        icon: DatabaseIcon,
        owns: ["/databases"],
        resource: "databases",
      },
      // Beside Databases for the same reason it is beside Projects: a
      // bucket belongs to the instance, and it is a thing you deploy
      // against rather than a thing the instance is wired to. It sits
      // here even when the store itself is somebody else's — where the
      // bytes are is not what decides which half of the sidebar a
      // screen belongs in.
      // "Object storage" rather than "Storage", which said nothing:
      // beside Databases in the same section it read as the place disks
      // are, and the URL stays /storage because a label is not a route.
      {
        href: "/storage",
        label: "Object storage",
        icon: HardDriveIcon,
        owns: ["/storage"],
        resource: "storage",
      },
      // Ready-made apps from the catalog. In Workspace, because an
      // install is a project and the apps and data inside it.
      {
        href: "/templates",
        label: "Templates",
        icon: LayoutTemplateIcon,
        owns: ["/templates"],
        resource: "templates",
      },
    ],
  },
  {
    label: "Platform",
    items: [
      // First in the section, because the others are configured
      // *through* it: a registry and a DNS account both name a
      // credential now rather than holding a secret of their own.
      {
        href: "/credentials",
        label: "Credentials",
        icon: KeyRoundIcon,
        owns: ["/credentials"],
        resource: "credentials",
      },
      // Beside Credentials, and for the same reason it is in this
      // section at all: who can reach this instance is a fact about
      // the instance, the same kind as which registry it pulls from.
      // What is *yours* — your password, the colours you see — is
      // under your own name at the foot of this list.
      { href: "/users", label: "Users", icon: UsersIcon, owns: ["/users"], admin: true },
      // Beside Users: what those people, and the keys they handed to
      // agents, actually did.
      { href: "/audit", label: "Audit log", icon: ScrollTextIcon, resource: "audit" },
      {
        href: "/registries",
        label: "Registries",
        icon: ContainerIcon,
        owns: ["/registries"],
        resource: ["registries", "registry"],
      },
      {
        href: "/git",
        label: "Git Providers",
        icon: GitBranchIcon,
        owns: ["/git"],
        resource: "git",
      },
      { href: "/dns", label: "DNS Providers", icon: GlobeIcon, owns: ["/dns"], resource: "dns" },
      {
        href: "/certificates",
        label: "Certificates",
        icon: ShieldCheckIcon,
        resource: "certificates",
      },
      // Here rather than beside Databases: what is on this screen is not
      // about any one database, and half of it is about databases that
      // no longer exist — which is the one thing a database's own tab
      // can never show.
      {
        href: "/backups",
        label: "Backups",
        icon: ArchiveIcon,
        owns: ["/backups"],
        resource: "backups",
      },
      // Next to Certificates rather than under Instance: both are about
      // how the outside reaches this machine, and both are read far
      // more often than they are changed.
      { href: "/firewall", label: "Firewall", icon: ShieldIcon, resource: "firewall" },
      // Above Instance, and the two read as a pair: the machines this
      // instance is made of, then the instance itself. A server is not
      // something you deploy *to* yet — when it is, this may well
      // belong beside Projects rather than here.
      {
        href: "/servers",
        label: "Servers",
        icon: ServerIcon,
        owns: ["/servers"],
        resource: "servers",
      },
      {
        href: "/components",
        label: "Components",
        icon: ServerCogIcon,
        owns: ["/components"],
        admin: true,
      },
      { href: "/settings", label: "Settings", icon: ServerCogIcon, resource: "settings" },
    ],
  },
  // There is no "You" section any more. It held one item, and what is
  // yours — how you sign in, what colour you see, and who else can get
  // in at all — is under your own name at the foot of this list rather
  // than filed beside what the instance is made of.
];

// Every destination the sidebar offers, flattened — what the command
// palette goes to. Derived rather than written again: a page added to
// the list above would otherwise be missing here, and this is the half
// nobody notices is missing.
const SCREENS = sections.flatMap((s) =>
  s.items.map((i) => ({ label: i.label, href: i.href, icon: i.icon })),
);

type NavItem = {
  href: string;
  label: string;
  owns?: string[];
  icon: typeof FolderTreeIcon;
  // What the screen is about: shown to somebody who can see any of it.
  resource?: string | string[];
  admin?: boolean;
};

// offered is whether the sidebar shows an entry at all. A screen somebody
// cannot open is a link to a refusal.
function offered(me: Me, item: NavItem) {
  if (item.admin) return me.role === "admin" && !me.grants;
  if (!item.resource) return true;
  const resources = Array.isArray(item.resource) ? item.resource : [item.resource];
  return resources.some((r) => can(me, r));
}

// Shell is every signed-in page: it resolves who you are before
// rendering, and sends you to sign in when the answer is nobody.
export function Shell({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);

  const [sessionError, setSessionError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt intentionally restarts session bootstrap after Retry
  useEffect(() => {
    let live = true;
    setSessionError(null);
    api
      .get<Me>("/users/me")
      .then((account) => {
        if (live) setMe(account);
      })
      .catch((error) => {
        if (!live) return;
        if (error?.status === 401) router.replace("/login");
        else
          setSessionError(
            error instanceof Error ? error.message : "Check your connection and try again.",
          );
      });
    return () => {
      live = false;
    };
  }, [router, attempt]);

  const updateSession = useCallback((change: Partial<Me>) => {
    setMe((current) => (current ? { ...current, ...change } : current));
  }, []);

  if (!me)
    return <SessionLoading error={sessionError} onRetry={() => setAttempt((value) => value + 1)} />;

  return (
    <SessionProvider me={me} update={updateSession}>
      <QueryProvider>
        {/* What changed, once, after an upgrade — and on demand from
            the menu below, which is why it wraps rather than sits
            beside: the item that opens it is three components down. */}
        <ReleaseNotes>
          <div className="dashboard-shell">
            <a className="dashboard-skip" href="#dashboard-content">
              Skip to content
            </a>
            <aside className="dashboard-sidebar">
              <Link href="/" aria-label="Cubeship home" className="dashboard-brand">
                <Wordmark className="text-sm" markClassName="size-6" />
              </Link>
              <Navigation me={me} />
              <div className="dashboard-account">
                <UserMenu me={me} />
              </div>
            </aside>

            <InstanceUpdate />

            <main className="dashboard-main">
              <CommandPalette screens={SCREENS} />
              <HeaderRail navigation={<MobileNavigation me={me} />}>
                <div
                  id="dashboard-content"
                  tabIndex={-1}
                  className="dashboard-container dashboard-content"
                >
                  {children}
                </div>
              </HeaderRail>
            </main>
          </div>
        </ReleaseNotes>
      </QueryProvider>
    </SessionProvider>
  );
}

function Navigation({ me, onNavigate }: { me: Me; onNavigate?: () => void }) {
  return (
    <nav className="dashboard-navigation" aria-label="Main navigation">
      {sections.map((section) => {
        const items = section.items.filter((item) => offered(me, item));
        if (!items.length) return null;
        return (
          <div key={section.label ?? "workspace"} className="dashboard-nav-section">
            <p className="dashboard-nav-label">{section.label ?? "Workspace"}</p>
            {items.map((item) => (
              <NavLink key={item.href} {...item} onNavigate={onNavigate} />
            ))}
          </div>
        );
      })}
    </nav>
  );
}

function MobileNavigation({ me }: { me: Me }) {
  const [open, setOpen] = useState(false);
  const pathname = usePathname();
  // biome-ignore lint/correctness/useExhaustiveDependencies: close the mobile drawer after a route change
  useEffect(() => {
    setOpen(false);
  }, [pathname]);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger className="dashboard-menu-toggle" aria-label="Open navigation">
        <MenuIcon className="size-5" />
      </DialogTrigger>
      <DialogContent className="dashboard-mobile-navigation">
        <DialogTitle className="sr-only">Navigation</DialogTitle>
        <Link href="/" className="dashboard-brand" onClick={() => setOpen(false)}>
          <Wordmark className="text-sm" markClassName="size-6" />
        </Link>
        <Navigation me={me} onNavigate={() => setOpen(false)} />
        <div className="dashboard-account">
          <UserMenu me={me} />
        </div>
      </DialogContent>
    </Dialog>
  );
}

function NavLink({
  href,
  label,
  owns = [],
  icon: Icon,
  onNavigate,
}: NavItem & { onNavigate?: () => void }) {
  const pathname = usePathname();
  const active =
    pathname === href || owns.some((p) => pathname === p || pathname.startsWith(`${p}/`));
  return (
    <Link
      href={href}
      onClick={onNavigate}
      aria-current={active ? "page" : undefined}
      className="dashboard-nav-link"
    >
      <Icon className="size-4 shrink-0" aria-hidden="true" />
      <span>{label}</span>
    </Link>
  );
}

function UserMenu({ me }: { me: Me }) {
  const router = useRouter();
  const [signingOut, setSigningOut] = useState(false);
  const releaseNotes = useReleaseNotes();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button type="button" className="dashboard-user-trigger">
            <UserAvatar person={me} className="size-8" />
            <span className="dashboard-user-name">
              <span>{personName(me)}</span>
              <small>{me.role === "admin" ? "Administrator" : "Your account"}</small>
            </span>
            <ChevronsUpDownIcon
              className="ml-auto size-3.5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          </button>
        }
      />
      {/* Out to the side, not up over the sidebar.
          
          The trigger is the last thing in a full-height column, so a
          menu below it has nowhere to go and Base UI flips it upward —
          over the navigation, which is the one part of the screen that
          is supposed to stay put. To the right it opens into the page,
          and `align="end"` keeps its own bottom on the trigger's so it
          grows away from the edge rather than into it. */}
      <DropdownMenuContent side="right" align="end" className="w-52">
        {/* GroupLabel is a Base UI group part and throws outside a Group. */}
        <DropdownMenuGroup>
          {/* The name on top and the address under it. A username is
              what a confirmation asks you to type and what `docker
              login` sends, so it stays visible and stays mono — but it
              is not what somebody is called. */}
          <DropdownMenuLabel className="text-xs">
            <span className="block truncate">{personName(me)}</span>
            <span className="block truncate font-mono text-[11px] text-subtle-foreground">
              {me.username}
              {me.role === "admin" && <span className="ml-1.5">· admin</span>}
            </span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem render={<Link href="/account" />}>
          <SettingsIcon />
          Your settings
        </DropdownMenuItem>
        {/* Reading them again is not an event, so it opens the history
            rather than whatever is unread. */}
        <DropdownMenuItem onClick={releaseNotes}>
          <SparklesIcon />
          Release notes
        </DropdownMenuItem>
        <GitHubStar />
        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={signingOut}
          onClick={async () => {
            if (signingOut) return;
            setSigningOut(true);
            try {
              await api.post("/auth/logout");
              router.replace("/login");
            } catch (error) {
              toast.error(message(error));
              setSigningOut(false);
            }
          }}
        >
          <LogOutIcon />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
