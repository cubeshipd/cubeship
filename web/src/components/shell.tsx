"use client";

import { cn } from "cn";
import {
  ContainerIcon,
  FolderTreeIcon,
  LogOutIcon,
  ServerCogIcon,
  UserRoundIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";
import { OrgProvider } from "@/components/org-context";
import { OrgSwitcher } from "@/components/org-switcher";
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

// `owns` is the rest of the section: the pages you reach from this
// entry that have no entry of their own. Projects is a section, not a
// page — a project, an environment and an app all live under it, and
// all four keep it lit. Adding a page under one is an edit here, not a
// new special case in the component below.
const nav = [
  {
    href: "/",
    label: "Projects",
    icon: FolderTreeIcon,
    owns: ["/projects", "/environments", "/apps"],
  },
  { href: "/registries", label: "Registries", icon: ContainerIcon },
  { href: "/settings", label: "Instance", icon: ServerCogIcon },
  { href: "/account", label: "Account", icon: UserRoundIcon },
];

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
      <OrgProvider>
        <div className="flex min-h-screen bg-background">
          <nav className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-border bg-card">
            <div className="border-b border-border">
              <OrgSwitcher />
            </div>

            <div className="flex-1 p-2">
              {nav.map((item) => (
                <NavLink key={item.href} {...item} />
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
    </SessionProvider>
  );
}

function NavLink({
  href,
  label,
  owns = [],
  icon: Icon,
}: {
  href: string;
  label: string;
  owns?: string[];
  icon: typeof FolderTreeIcon;
}) {
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
