"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { api, ApiError, type Me } from "@/lib/api";

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="mb-3 block">
      <span className="mb-1.5 block text-xs text-muted">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-muted">{hint}</span>}
    </label>
  );
}

export const inputClass =
  "w-full rounded-md border border-line bg-ink px-3 py-2 text-sm outline-none focus:border-brand";

export function Button({
  children,
  variant = "default",
  className = "",
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" | "danger" }) {
  const styles = {
    default: "border-line bg-raised hover:border-brand",
    primary: "border-brand bg-brand text-white hover:opacity-90",
    danger: "border-line bg-raised text-bad hover:border-bad",
  }[variant];
  return (
    <button
      {...rest}
      className={`rounded-md border px-3.5 py-2 text-sm disabled:cursor-default disabled:opacity-50 ${styles} ${className}`}
    >
      {children}
    </button>
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={`mb-3.5 rounded-lg border border-line bg-panel p-4 ${className}`}>{children}</div>
  );
}

export function ErrorNote({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <div className="mb-3.5 rounded-md border border-bad/50 bg-bad/10 px-3 py-2.5 text-sm text-bad">
      {error}
    </div>
  );
}

const statusTone: Record<string, string> = {
  running: "text-good border-good/40",
  succeeded: "text-good border-good/40",
  deploying: "text-warn border-warn/40",
  pending: "text-warn border-warn/40",
  failed: "text-bad border-bad/40",
  stopped: "text-bad border-bad/40",
};

export function Status({ value }: { value: string }) {
  return (
    <span
      className={`inline-block rounded-full border px-2 py-0.5 text-[11px] ${statusTone[value] ?? "border-line text-muted"}`}
    >
      {value}
    </span>
  );
}

// message pulls the text out of whatever a failed call threw. Every
// error the daemon returns is plain text, so there is nothing to parse.
export function message(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return String(err);
}

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
    <div className="flex min-h-screen">
      <nav className="w-56 shrink-0 border-r border-line bg-panel p-4">
        <div className="mb-6 px-2.5 font-semibold">Cubeship</div>
        <NavLink href="/">Apps</NavLink>
        <NavLink href="/organizations">Organizations</NavLink>
        <NavLink href="/registries">Registries</NavLink>
        <NavLink href="/settings">Instance</NavLink>
        <NavLink href="/account">Account</NavLink>
        <div className="mt-6 border-t border-line px-2.5 pt-4 text-xs text-muted">
          <div>{me.username}</div>
          <button
            className="mt-2 text-muted hover:text-body"
            onClick={async () => {
              await api.post("/auth/logout");
              router.replace("/login");
            }}
          >
            Sign out
          </button>
        </div>
      </nav>
      <main className="max-w-5xl flex-1 p-8">{children}</main>
    </div>
  );
}

function NavLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <Link
      href={href}
      className="mb-0.5 block rounded-md px-2.5 py-1.5 text-sm text-muted hover:bg-raised hover:text-body"
    >
      {children}
    </Link>
  );
}

export function PageHeader({ title, sub }: { title: string; sub?: string }) {
  return (
    <header className="mb-6">
      <h1 className="text-xl font-semibold">{title}</h1>
      {sub && <p className="mt-1 text-sm text-muted">{sub}</p>}
    </header>
  );
}
