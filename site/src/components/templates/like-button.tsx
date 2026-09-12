"use client";

import { Heart } from "lucide-react";
import { usePathname } from "next/navigation";
import { useState } from "react";

export function LikeButton({
  slug,
  initialCount,
  initialLiked,
}: {
  slug: string;
  initialCount: number;
  initialLiked: boolean;
}) {
  const pathname = usePathname();
  const [count, setCount] = useState(initialCount);
  const [liked, setLiked] = useState(initialLiked);
  const [pending, setPending] = useState(false);

  async function toggle() {
    if (pending) return;
    setPending(true);
    // Optimistic: flipped before the request settles, and rolled back
    // only if the server disagrees.
    const next = !liked;
    setLiked(next);
    setCount((current) => current + (next ? 1 : -1));

    try {
      const response = await fetch(`/api/v1/templates/${slug}/like`, {
        method: next ? "PUT" : "DELETE",
      });
      if (response.status === 401) {
        window.location.href = `/api/auth/github?next=${encodeURIComponent(pathname)}`;
        return;
      }
      if (!response.ok) throw new Error(`the server answered ${response.status}`);
      const body = (await response.json()) as { likesCount: number; liked: boolean };
      setCount(body.likesCount);
      setLiked(body.liked);
    } catch {
      setLiked(!next);
      setCount((current) => current - (next ? 1 : -1));
    } finally {
      setPending(false);
    }
  }

  return (
    <button
      type="button"
      onClick={toggle}
      disabled={pending}
      aria-pressed={liked}
      className={`hud-frame flex items-center gap-2 border px-3 py-2 font-mono text-sm transition-colors disabled:opacity-60 ${
        liked
          ? "border-primary text-primary"
          : "border-fd-border text-fd-muted-foreground hover:border-primary hover:text-primary"
      }`}
    >
      <Heart className="size-4" fill={liked ? "currentColor" : "none"} aria-hidden />
      {count}
    </button>
  );
}
