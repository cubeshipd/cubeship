"use client";

import { SiGithub } from "@icons-pack/react-simple-icons";
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
import { BranchedMenu } from "@/components/branched-menu";
import { Wordmark } from "@/components/brand";
import { CommandPalette } from "@/components/command-palette";
import { abbreviateGitHubStars, GitHubStar, useGitHubStars } from "@/components/github-star";
import { GlideSelect } from "@/components/glide-select";
import { HeaderRail } from "@/components/header-rail";
import { InstanceUpdate } from "@/components/instance-update";
import Link from "@/components/navigation-link";
import { SessionLoading } from "@/components/page-loading";
import { QueryProvider } from "@/components/query-provider";
import { ReleaseNotes, useReleaseNotes } from "@/components/release-notes";
import { ScrollToTop } from "@/components/scroll-to-top";
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

// Sidebar groups also feed the command palette below. `owns` keeps
// nested pages active under the entry that contains them.
export const sections: { label: string; items: NavItem[] }[] = [
  {
    label: "General",
    items: [{ href: "/", label: "Overview", icon: ActivityIcon }],
  },
  {
    label: "Workloads",
    items: [
      {
        href: "/servers",
        label: "Nodes",
        icon: ServerIcon,
        owns: ["/servers"],
        resource: "servers",
      },
      {
        href: "/workloads",
        label: "Containers",
        icon: ContainerIcon,
        owns: ["/workloads"],
        resource: ["apps", "databases", "storage"],
      },
      {
        href: "/projects",
        label: "Projects",
        icon: FolderTreeIcon,
        owns: ["/projects", "/environments", "/apps"],
        resource: ["projects", "apps"],
      },
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
    label: "Data",
    items: [
      {
        href: "/databases",
        label: "Databases",
        icon: DatabaseIcon,
        owns: ["/databases"],
        resource: "databases",
      },
      {
        href: "/storage",
        label: "Object storage",
        icon: HardDriveIcon,
        owns: ["/storage"],
        resource: "storage",
      },
      {
        href: "/backups",
        label: "Backups",
        icon: ArchiveIcon,
        owns: ["/backups"],
        resource: "backups",
      },
    ],
  },
  {
    label: "Network",
    items: [
      { href: "/dns", label: "DNS Providers", icon: GlobeIcon, owns: ["/dns"], resource: "dns" },
      { href: "/firewall", label: "Firewall", icon: ShieldIcon, resource: "firewall" },
    ],
  },
  {
    label: "Config",
    items: [
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
      {
        href: "/components",
        label: "Components",
        icon: ServerCogIcon,
        owns: ["/components"],
        admin: true,
      },
      { href: "/settings", label: "Settings", icon: SettingsIcon, resource: "settings" },
    ],
  },
  {
    label: "Security",
    items: [
      {
        href: "/credentials",
        label: "Credentials",
        icon: KeyRoundIcon,
        owns: ["/credentials"],
        resource: "credentials",
      },
      {
        href: "/certificates",
        label: "Certificates",
        icon: ShieldCheckIcon,
        resource: "certificates",
      },
    ],
  },
  {
    label: "Access",
    items: [
      { href: "/users", label: "Users", icon: UsersIcon, owns: ["/users"], admin: true },
      { href: "/audit", label: "Audit log", icon: ScrollTextIcon, resource: "audit" },
    ],
  },
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
  const pathname = usePathname();
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
              <div className="dashboard-brand">
                <Wordmark className="text-sm" markClassName="size-6" />
              </div>
              <Navigation me={me} />
              <div className="dashboard-account">
                <UserMenu me={me} />
              </div>
            </aside>

            <InstanceUpdate />

            <main className="dashboard-main">
              <CommandPalette screens={SCREENS} />
              {pathname === "/projects" ||
              pathname.startsWith("/projects/") ||
              pathname === "/databases" ||
              pathname.startsWith("/databases/") ||
              pathname === "/storage" ||
              pathname.startsWith("/storage/") ||
              pathname === "/templates" ||
              pathname.startsWith("/templates/") ? (
                <div
                  id="dashboard-content"
                  tabIndex={-1}
                  className="dashboard-container dashboard-content"
                >
                  {(pathname === "/templates" || pathname.startsWith("/templates/")) && (
                    <div className="-mt-4 mb-3 hidden justify-end max-[760px]:flex">
                      <MobileNavigation me={me} />
                    </div>
                  )}
                  {children}
                </div>
              ) : (
                <HeaderRail
                  navigation={<MobileNavigation me={me} />}
                  bare={
                    pathname === "/workloads" || pathname === "/servers" || pathname === "/backups"
                  }
                >
                  <div
                    id="dashboard-content"
                    tabIndex={-1}
                    className="dashboard-container dashboard-content"
                  >
                    {children}
                  </div>
                </HeaderRail>
              )}
              <ScrollToTop />
            </main>
          </div>
        </ReleaseNotes>
      </QueryProvider>
    </SessionProvider>
  );
}

function Navigation({ me, onNavigate }: { me: Me; onNavigate?: () => void }) {
  const pathname = usePathname();
  const items = sections
    .map((section) => ({
      label: section.label ?? "Workspace",
      children: section.items
        .filter((item) => offered(me, item))
        .map((item) => {
          const Icon = item.icon;
          return {
            value: item.href,
            label: item.label,
            icon: <Icon className="size-4" />,
          };
        }),
    }))
    .filter((section) => section.children.length > 0);
  const activeValue =
    sections
      .flatMap((section) => section.items)
      .find(
        (item) =>
          offered(me, item) &&
          (pathname === item.href ||
            item.owns?.some((path) => pathname === path || pathname.startsWith(`${path}/`))),
      )?.href ?? "";

  return (
    <BranchedMenu
      className="dashboard-navigation"
      items={items}
      activeValue={activeValue}
      onSelect={onNavigate}
    />
  );
}

export function MobileNavigation({ me }: { me: Me }) {
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
        <div className="dashboard-brand">
          <Wordmark className="text-sm" markClassName="size-6" />
        </div>
        <Navigation me={me} onNavigate={() => setOpen(false)} />
        <div className="dashboard-account">
          <UserMenu me={me} mobile />
        </div>
      </DialogContent>
    </Dialog>
  );
}

function UserMenu({ me, mobile = false }: { me: Me; mobile?: boolean }) {
  return mobile ? <MobileUserMenu me={me} /> : <DesktopUserMenu me={me} />;
}

function useSignOut() {
  const router = useRouter();
  const [signingOut, setSigningOut] = useState(false);

  async function signOut() {
    if (signingOut) return;
    setSigningOut(true);
    try {
      await api.post("/auth/logout");
      router.replace("/login");
    } catch (error) {
      toast.error(message(error));
      setSigningOut(false);
    }
  }

  return { signingOut, signOut };
}

function MobileUserMenu({ me }: { me: Me }) {
  const router = useRouter();
  const releaseNotes = useReleaseNotes();
  const { signingOut, signOut } = useSignOut();
  const stars = useGitHubStars();

  return (
    <GlideSelect
      className="w-full"
      value=""
      ariaLabel="Account menu"
      align="right"
      triggerClassName="dashboard-user-trigger"
      triggerContent={
        <>
          <UserAvatar person={me} className="size-8" />
          <span className="dashboard-user-name">
            <span>{personName(me)}</span>
            <small>{me.role === "admin" ? "Administrator" : "Your account"}</small>
          </span>
          <ChevronsUpDownIcon
            className="ml-auto size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
        </>
      }
      options={[
        {
          value: "account",
          label: (
            <span className="inline-flex items-center gap-2">
              <SettingsIcon className="size-4" /> Your settings
            </span>
          ),
        },
        {
          value: "release-notes",
          label: (
            <span className="inline-flex items-center gap-2">
              <SparklesIcon className="size-4" /> Release notes
            </span>
          ),
        },
        {
          value: "github",
          label: (
            <span className="inline-flex items-center gap-2">
              <SiGithub className="size-4" /> Star on GitHub
              {stars !== null && (
                <span className="ml-auto font-mono text-xs text-muted-foreground">
                  {abbreviateGitHubStars(stars)}
                </span>
              )}
            </span>
          ),
        },
        {
          value: "sign-out",
          label: (
            <span className="inline-flex items-center gap-2 text-destructive">
              <LogOutIcon className="size-4" /> {signingOut ? "Signing out…" : "Sign out"}
            </span>
          ),
        },
      ]}
      onChange={(action) => {
        if (action === "account") router.push("/account");
        else if (action === "release-notes") releaseNotes();
        else if (action === "github")
          window.open("https://github.com/cubeshipd/cubeship", "_blank", "noopener,noreferrer");
        else if (action === "sign-out") void signOut();
      }}
    />
  );
}

function DesktopUserMenu({ me }: { me: Me }) {
  const { signingOut, signOut } = useSignOut();
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
      <DropdownMenuContent side="right" align="end" className="w-52">
        <DropdownMenuGroup>
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
          <SettingsIcon /> Your settings
        </DropdownMenuItem>
        <DropdownMenuItem onClick={releaseNotes}>
          <SparklesIcon /> Release notes
        </DropdownMenuItem>
        <GitHubStar />
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={signingOut} onClick={() => void signOut()}>
          <LogOutIcon /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
