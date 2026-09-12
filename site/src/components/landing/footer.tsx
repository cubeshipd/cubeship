import { Wordmark } from "@/components/brand";
import { githubUrl } from "@/lib/shared";

// A group is a column; adding a link is a line, adding a group is a
// column, and the grid takes as many as there are.
const groups: { title: string; links: { text: string; href: string }[] }[] = [
  {
    title: "Product",
    links: [
      { text: "Docs", href: "/docs" },
      { text: "Install", href: "/docs/getting-started/install" },
      { text: "The CLI", href: "/docs/cli" },
      { text: "The API", href: "/docs/api" },
      { text: "MCP", href: "/docs/mcp" },
    ],
  },
  {
    title: "Compare",
    links: [
      { text: "vs Dokploy", href: "/vs/dokploy" },
      { text: "vs Coolify", href: "/vs/coolify" },
    ],
  },
  {
    title: "Project",
    links: [
      { text: "GitHub", href: githubUrl },
      { text: "Changelog", href: `${githubUrl}/blob/master/CHANGELOG.md` },
      { text: "Security", href: `${githubUrl}/blob/master/SECURITY.md` },
      { text: "License", href: `${githubUrl}/blob/master/LICENSE` },
      { text: "Trademark", href: `${githubUrl}/blob/master/TRADEMARK.md` },
    ],
  },
];

export function Footer() {
  return (
    <footer className="mx-auto max-w-6xl px-6 py-14">
      <div className="grid gap-10 md:grid-cols-[1.2fr_2fr]">
        <div>
          <Wordmark />
          <p className="mt-3 max-w-xs text-subtle-foreground text-xs leading-relaxed">
            Apache-2.0. Use it, run it, modify it, run your company on it. The name is the one thing
            not in the grant.
          </p>
        </div>
        {/* auto-fit only works with every track flexible, which is why
            the groups have a grid of their own beside the wordmark. */}
        <div className="grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-8">
          {groups.map((g) => (
            <nav key={g.title} aria-label={g.title}>
              <p className="label text-subtle-foreground">{g.title}</p>
              <ul className="mt-3 space-y-2">
                {g.links.map((l) => (
                  <li key={l.text}>
                    <a
                      href={l.href}
                      className="text-muted-foreground text-sm transition-colors hover:text-primary"
                    >
                      {l.text}
                    </a>
                  </li>
                ))}
              </ul>
            </nav>
          ))}
        </div>
      </div>
    </footer>
  );
}
