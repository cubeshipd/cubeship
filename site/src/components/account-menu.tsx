"use client";

import { useEffect, useState } from "react";

type Me = { id: number; login: string; avatarUrl: string | null; role: string } | null;

export function AccountMenu() {
  const [me, setMe] = useState<Me>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    fetch("/api/v1/me")
      .then((response) => response.json())
      .then((body) => setMe(body.user))
      .catch(() => setMe(null));
  }, []);

  if (!me) {
    return (
      <a
        href={`/api/auth/github?next=${encodeURIComponent(typeof window === "undefined" ? "/templates" : window.location.pathname)}`}
        className="label text-fd-muted-foreground hover:text-fd-foreground"
      >
        Sign in
      </a>
    );
  }

  return (
    <div className="relative">
      <button type="button" onClick={() => setOpen(!open)} aria-label={`Account: ${me.login}`}>
        {me.avatarUrl ? (
          // biome-ignore lint/performance/noImgElement: an external GitHub avatar, not one of our own assets.
          <img src={me.avatarUrl} alt="" className="size-7 border border-fd-border" />
        ) : (
          <span className="label">{me.login}</span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-50 mt-2 w-44 border border-fd-border bg-fd-background p-2 text-sm">
          <a href={`/u/${me.login}`} className="block px-2 py-1 hover:text-fd-foreground">
            My templates
          </a>
          <a href="/me/templates" className="block px-2 py-1 hover:text-fd-foreground">
            Drafts
          </a>
          <button
            type="button"
            className="block w-full px-2 py-1 text-left hover:text-fd-foreground"
            onClick={async () => {
              await fetch("/api/auth/signout", { method: "POST" });
              window.location.reload();
            }}
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  );
}
