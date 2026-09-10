"use client";

import { cn } from "cn";
import {
  ActivityIcon,
  ContainerIcon,
  DatabaseIcon,
  FolderTreeIcon,
  GitBranchIcon,
  GlobeIcon,
  HardDriveIcon,
  KeyRoundIcon,
  LogOutIcon,
  ServerCogIcon,
  ServerIcon,
  SettingsIcon,
  ShieldCheckIcon,
  ShieldIcon,
  SparklesIcon,
  UsersIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";
import { Wordmark } from "@/components/brand";
import { GitHubLink } from "@/components/github-link";
import { InstanceUpdate } from "@/components/instance-update";
import { QueryProvider } from "@/components/query-provider";
import { ReleaseNotes, useReleaseNotes } from "@/components/release-notes";
import { SessionProvider } from "@/components/session-context";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { api, type Me } from "@/lib/api";

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
const sections: { label?: string; items: NavItem[] }[] = [
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
      },
      // Beside Projects rather than under Platform: a database belongs
      // to the instance, but it is a thing you deploy against, not a
      // thing the instance is wired to. It is opened as often as an app
      // is, which is what the Platform section is not for.
      { href: "/databases", label: "Databases", icon: DatabaseIcon, owns: ["/databases"] },
      // Beside Databases for the same reason it is beside Projects: a
      // bucket belongs to the instance, and it is a thing you deploy
      // against rather than a thing the instance is wired to. It sits
      // here even when the store itself is somebody else's — where the
      // bytes are is not what decides which half of the sidebar a
      // screen belongs in.
      { href: "/storage", label: "Storage", icon: HardDriveIcon, owns: ["/storage"] },
    ],
  },
  {
    label: "Platform",
    items: [
      // First in the section, because the others are configured
      // *through* it: a registry and a DNS account both name a
      // credential now rather than holding a secret of their own.
      { href: "/credentials", label: "Credentials", icon: KeyRoundIcon, owns: ["/credentials"] },
      // Beside Credentials, and for the same reason it is in this
      // section at all: who can reach this instance is a fact about
      // the instance, the same kind as which registry it pulls from.
      // What is *yours* — your password, the colours you see — is
      // under your own name at the foot of this list.
      { href: "/users", label: "Users", icon: UsersIcon, owns: ["/users"] },
      { href: "/registries", label: "Registries", icon: ContainerIcon, owns: ["/registries"] },
      { href: "/git", label: "Git Providers", icon: GitBranchIcon, owns: ["/git"] },
      { href: "/dns", label: "DNS Providers", icon: GlobeIcon, owns: ["/dns"] },
      { href: "/certificates", label: "Certificates", icon: ShieldCheckIcon },
      // Next to Certificates rather than under Instance: both are about
      // how the outside reaches this machine, and both are read far
      // more often than they are changed.
      { href: "/firewall", label: "Firewall", icon: ShieldIcon },
      // Above Instance, and the two read as a pair: the machines this
      // instance is made of, then the instance itself. A server is not
      // something you deploy *to* yet — when it is, this may well
      // belong beside Projects rather than here.
      { href: "/servers", label: "Servers", icon: ServerIcon, owns: ["/servers"] },
      { href: "/settings", label: "Instance", icon: ServerCogIcon },
    ],
  },
  // There is no "You" section any more. It held one item, and what is
  // yours — how you sign in, what colour you see, and who else can get
  // in at all — is under your own name at the foot of this list rather
  // than filed beside what the instance is made of.
];

type NavItem = {
  href: string;
  label: string;
  owns?: string[];
  icon: typeof FolderTreeIcon;
};

// Shell is every signed-in page: it resolves who you are before
// rendering, and sends you to sign in when the answer is nobody.
export function Shell({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    api
      .get<Me>("/users/me")
      .then(setMe)
      .catch(() => router.replace("/login"));
  }, [router]);

  if (!me) return null;

  return (
    <SessionProvider me={me}>
      <QueryProvider>
        {/* What changed, once, after an upgrade — and on demand from
            the menu below, which is why it wraps rather than sits
            beside: the item that opens it is three components down. */}
        <ReleaseNotes>
          <div className="flex min-h-screen bg-background">
            <nav className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-border bg-card">
              {/* A link rather than a plate: the mark is the way back to
                the projects grid, which is where every other product
                puts it and where a click on it is aimed. */}
              <Link
                href="/"
                className="flex h-14 items-center border-b border-border px-4 text-primary transition-opacity hover:opacity-80"
              >
                <Wordmark className="text-xs" markClassName="size-5" />
              </Link>

              <div className="flex-1 p-2">
                {sections.map((section, i) => (
                  <div key={section.label ?? "workspace"} className={i > 0 ? "mt-5" : undefined}>
                    {/* A heading rather than a rule: a line says these
                        are apart, a word says what the other side is. */}
                    {section.label && (
                      <p className="mb-1 px-3 text-[10px] font-semibold tracking-[0.18em] text-subtle-foreground uppercase">
                        {section.label}
                      </p>
                    )}
                    {section.items.map((item) => (
                      <NavLink key={item.href} {...item} />
                    ))}
                  </div>
                ))}
              </div>

              <div className="flex items-center gap-1 border-t border-border p-2">
                <UserMenu me={me} />
                {/* The one link out of the instance, and the one place it
                  is: it was in the menu as well, which is two doors to
                  one room. */}
                <GitHubLink />
              </div>
            </nav>

            <InstanceUpdate />

            <main className="min-w-0 flex-1">
              <div className="mx-auto max-w-5xl px-8 py-8">{children}</div>
            </main>
          </div>
        </ReleaseNotes>
      </QueryProvider>
    </SessionProvider>
  );
}

function NavLink({ href, label, owns = [], icon: Icon }: NavItem) {
  const pathname = usePathname();
  const active =
    pathname === href || owns.some((p) => pathname === p || pathname.startsWith(`${p}/`));

  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative flex items-center gap-2.5 px-3 py-2 text-[13px] font-medium tracking-[0.08em] uppercase transition-colors",
        active
          ? "bg-primary/8 text-foreground"
          : "text-muted-foreground hover:bg-secondary hover:text-foreground",
      )}
    >
      {/* The lit rail down the left edge is what says "you are here";
          the background tint alone is too quiet at this contrast. */}
      {active && (
        <span
          aria-hidden="true"
          className="absolute inset-y-0 left-0 w-0.5 bg-primary shadow-[0_0_10px_var(--primary)]"
        />
      )}
      <Icon className={cn("size-4 shrink-0", active ? "text-primary" : "text-subtle-foreground")} />
      {label}
    </Link>
  );
}

function UserMenu({ me }: { me: Me }) {
  const router = useRouter();
  const releaseNotes = useReleaseNotes();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="flex min-w-0 flex-1 items-center gap-2.5 border border-transparent px-2 py-2 text-left text-sm transition-colors hover:border-border hover:bg-secondary"
          >
            <span className="flex size-6 shrink-0 items-center justify-center border border-primary/40 bg-primary/10 font-mono text-[11px] text-primary">
              {me.username.slice(0, 2)}
            </span>
            <span className="truncate font-mono text-xs">{me.username}</span>
          </button>
        }
      />
      <DropdownMenuContent align="start" className="w-52">
        {/* GroupLabel is a Base UI group part and throws outside a Group. */}
        <DropdownMenuGroup>
          <DropdownMenuLabel className="font-mono text-xs">
            {me.username}
            {me.role === "admin" && <span className="ml-1.5 text-subtle-foreground">· admin</span>}
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
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={async () => {
            await api.post("/auth/logout");
            router.replace("/login");
          }}
        >
          <LogOutIcon />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
