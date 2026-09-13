"use client";

import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

type Me = { id: number; login: string; avatarUrl: string | null; role: string } | null;

// Every state takes the same box, so the header does not move when the session
// arrives after the page has painted.
const BOX = "flex h-7 w-20 items-center justify-end";

export function AccountMenu() {
  // undefined until /api/v1/me answers, so nothing is shown that the answer replaces.
  const [me, setMe] = useState<Me | undefined>(undefined);
  const [open, setOpen] = useState(false);
  // Read from the router, not window: the server renders this link too, and a
  // server-side guess sends everyone back to the catalog after signing in.
  const pathname = usePathname();

  useEffect(() => {
    fetch("/api/v1/me")
      .then((response) => response.json())
      .then((body) => setMe(body.user))
      .catch(() => setMe(null));
  }, []);

  if (me === undefined) return <div className={BOX} aria-hidden="true" />;

  if (!me) {
    return (
      <div className={BOX}>
        <a
          href={`/api/auth/github?next=${encodeURIComponent(pathname)}`}
          className="label text-fd-muted-foreground hover:text-fd-foreground"
        >
          Sign in
        </a>
      </div>
    );
  }

  return (
    <div className={`relative ${BOX}`}>
      <button type="button" onClick={() => setOpen(!open)} aria-label={`Account: ${me.login}`}>
        {me.avatarUrl ? (
          // biome-ignore lint/performance/noImgElement: an external GitHub avatar, not one of our own assets.
          <img src={me.avatarUrl} alt="" className="size-7 border border-fd-border" />
        ) : (
          <span className="label block max-w-20 truncate">{me.login}</span>
        )}
      </button>
      {open && (
        <div className="absolute top-full right-0 z-50 mt-2 w-44 border border-fd-border bg-fd-background p-2 text-sm">
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
