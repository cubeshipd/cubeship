import { ArrowUpRight } from "lucide-react";
import Link from "next/link";
import { Wordmark } from "@/components/brand";
import { githubUrl } from "@/lib/shared";

const groups = [
  {
    title: "Platform",
    links: [
      { text: "Deployments", href: "/docs/apps" },
      { text: "Databases", href: "/docs/databases" },
      { text: "Templates", href: "/templates" },
      { text: "Clusters", href: "/docs/servers" },
      { text: "MCP", href: "/docs/mcp" },
    ],
  },
  {
    title: "Resources",
    links: [
      { text: "Documentation", href: "/docs" },
      { text: "Installation", href: "/docs/getting-started/install" },
      { text: "CLI & API", href: "/docs/cli" },
      { text: "vs Coolify", href: "/vs/coolify" },
      { text: "vs Dokploy", href: "/vs/dokploy" },
    ],
  },
  {
    title: "Open source",
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
    <footer className="site-container site-footer">
      <div className="footer-main">
        <div className="footer-brand">
          <Link href="/" aria-label="Cubeship home">
            <Wordmark />
          </Link>
          <p>The self-hosted platform for people who build. Your servers. Your rules.</p>
        </div>
        <div className="footer-links">
          {groups.map((group) => (
            <nav key={group.title} aria-label={group.title}>
              <h3>{group.title}</h3>
              <ul>
                {group.links.map((link) => (
                  <li key={link.text}>
                    <a href={link.href}>{link.text}</a>
                  </li>
                ))}
              </ul>
            </nav>
          ))}
        </div>
      </div>
      <div className="footer-bottom">
        <span>Open source under Apache 2.0.</span>
        <a href={githubUrl} data-track="github" data-track-location="footer">
          Built in the open. Made to be yours. <ArrowUpRight size={12} />
        </a>
      </div>
    </footer>
  );
}
