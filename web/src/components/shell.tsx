"use client";

import { cn } from "cn";
import {
  ContainerIcon,
  FolderTreeIcon,
  GitBranchIcon,
  GlobeIcon,
  LogOutIcon,
  ServerCogIcon,
  UserRoundIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";
import { OrgProvider } from "@/components/org-context";
import { OrgSwitcher } from "@/components/org-switcher";
import { QueryProvider } from "@/components/query-provider";
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
      {
        href: "/",
        label: "Projects",
        icon: FolderTreeIcon,
        owns: ["/projects", "/environments", "/apps"],
      },
    ],
  },
  {
    label: "Platform",
    items: [
      { href: "/registries", label: "Registries", icon: ContainerIcon, owns: ["/registries"] },
      { href: "/git", label: "Git Providers", icon: GitBranchIcon, owns: ["/git"] },
      { href: "/dns", label: "DNS Providers", icon: GlobeIcon, owns: ["/dns"] },
      { href: "/settings", label: "Instance", icon: ServerCogIcon },
    ],
  },
  {
    label: "You",
    items: [{ href: "/account", label: "Account", icon: UserRoundIcon }],
  },
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
        <OrgProvider>
          <div className="flex min-h-screen bg-background">
            <nav className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-border bg-card">
              <div className="border-b border-border">
                <OrgSwitcher />
              </div>

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

              <div className="border-t border-border p-2">
                <UserMenu me={me} />
              </div>
            </nav>

            <main className="min-w-0 flex-1">
              <div className="mx-auto max-w-5xl px-8 py-8">{children}</div>
            </main>
          </div>
        </OrgProvider>
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
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="flex w-full items-center gap-2.5 border border-transparent px-2 py-2 text-left text-sm transition-colors hover:border-border hover:bg-secondary"
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
            {me.is_super_admin && (
              <span className="ml-1.5 text-subtle-foreground">· super admin</span>
            )}
          </DropdownMenuLabel>
        </DropdownMenuGroup>
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
