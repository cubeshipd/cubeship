import { Wordmark } from "@/components/brand";
import { githubUrl } from "@/lib/shared";

const links = [
  { text: "Docs", href: "/docs" },
  { text: "vs Dokploy", href: "/vs/dokploy" },
  { text: "vs Coolify", href: "/vs/coolify" },
  { text: "GitHub", href: githubUrl },
  { text: "Changelog", href: `${githubUrl}/blob/master/CHANGELOG.md` },
  { text: "Security", href: `${githubUrl}/blob/master/SECURITY.md` },
  { text: "License", href: `${githubUrl}/blob/master/LICENSE` },
];

export function Footer() {
  return (
    <footer className="mx-auto flex max-w-6xl flex-col gap-6 px-6 py-10 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <Wordmark />
        <p className="mt-3 max-w-md text-subtle-foreground text-xs leading-relaxed">
          Apache-2.0. Use it, run it, modify it, run your company on it. The name is the one thing
          not in the grant.
        </p>
      </div>
      <nav className="flex flex-wrap gap-x-6 gap-y-2">
        {links.map((l) => (
          <a
            key={l.text}
            href={l.href}
            className="label text-muted-foreground transition-colors hover:text-primary"
          >
            {l.text}
          </a>
        ))}
      </nav>
    </footer>
  );
}
