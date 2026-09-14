"use client";

import { ArrowUpRight, Menu, X } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { githubUrl } from "@/lib/shared";
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
        <Link href="/" aria-label="Cubeship home" onClick={() => setOpen(false)}>
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
          <a className="header-github" href={githubUrl}>
            GitHub <ArrowUpRight size={14} />
          </a>
          <Link href="/docs/getting-started/install" className="button-small">
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
        <a href={githubUrl} onClick={() => setOpen(false)}>
          GitHub <ArrowUpRight size={16} />
        </a>
        <Link href="/docs/getting-started/install" onClick={() => setOpen(false)}>
          Install Cubeship <ArrowUpRight size={16} />
        </Link>
      </nav>
    </header>
  );
}
