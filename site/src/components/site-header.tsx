"use client";

import { ArrowUpRight, Heart, Menu, X } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { githubUrl, sponsorUrl } from "@/lib/shared";
import { Wordmark } from "./brand";

const links = [
  { href: "/#product", label: "Product" },
  { href: "/templates", label: "Templates" },
  { href: "/docs", label: "Documentation" },
];

export function SiteHeader() {
  const [open, setOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const pathname = usePathname();
  useEffect(() => {
    if (!open) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        menuButton.current?.focus();
      }
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [open]);
  return (
    <header className="site-header">
      <div className="site-container header-inner">
        <Link
          className="flex items-center"
          href="/"
          aria-label="Cubeship home"
          onClick={() => setOpen(false)}
        >
          <Wordmark />
        </Link>
        <nav className="desktop-navigation" aria-label="Main navigation">
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              aria-current={
                link.href !== "/#product" && pathname.startsWith(link.href) ? "page" : undefined
              }
            >
              {link.label}
            </Link>
          ))}
        </nav>
        <div className="header-actions">
          <a
            className="header-sponsor"
            href={sponsorUrl}
            aria-label="Sponsor lucasaarch on GitHub"
            data-track="sponsor"
            data-track-location="header"
          >
            <Heart size={14} aria-hidden="true" /> Sponsor
          </a>
          <a
            className="header-github"
            href={githubUrl}
            data-track="github"
            data-track-location="header"
          >
            GitHub <ArrowUpRight size={14} />
          </a>
          <Link
            href="/docs/getting-started/install"
            className="button-small"
            data-track="install"
            data-track-location="header"
          >
            Get started <ArrowUpRight size={14} />
          </Link>
        </div>
        <button
          ref={menuButton}
          type="button"
          className="mobile-menu-toggle"
          aria-expanded={open}
          aria-controls="mobile-navigation"
          aria-label={open ? "Close navigation" : "Open navigation"}
          onClick={() => setOpen(!open)}
        >
          {open ? <X size={22} /> : <Menu size={22} />}
        </button>
      </div>
      <nav
        id="mobile-navigation"
        className="mobile-navigation"
        aria-label="Mobile navigation"
        hidden={!open}
      >
        {links.map((link) => (
          <Link key={link.href} href={link.href} onClick={() => setOpen(false)}>
            {link.label}
          </Link>
        ))}
        <a
          href={githubUrl}
          onClick={() => setOpen(false)}
          data-track="github"
          data-track-location="mobile-menu"
        >
          GitHub <ArrowUpRight size={16} />
        </a>
        <a
          className="mobile-sponsor"
          href={sponsorUrl}
          onClick={() => setOpen(false)}
          data-track="sponsor"
          data-track-location="mobile-menu"
        >
          <Heart size={16} aria-hidden="true" /> Sponsor
        </a>
        <Link
          href="/docs/getting-started/install"
          onClick={() => setOpen(false)}
          data-track="install"
          data-track-location="mobile-menu"
        >
          Install Cubeship <ArrowUpRight size={16} />
        </Link>
      </nav>
    </header>
  );
}
