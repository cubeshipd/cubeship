import { Star } from "lucide-react";
import { GitHubIcon } from "@/components/icons";
import { gitConfig, githubUrl } from "@/lib/shared";

// Rendered on the server, an hour at a time. GitHub answers without a
// token at 60 requests an hour per address, which a cached hour never
// approaches; an answer that does not come leaves the count off and the
// link intact.
async function stars(): Promise<number | null> {
  try {
    const res = await fetch(`https://api.github.com/repos/${gitConfig.user}/${gitConfig.repo}`, {
      headers: { Accept: "application/vnd.github+json" },
      next: { revalidate: 3600 },
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { stargazers_count?: number };
    return typeof body.stargazers_count === "number" ? body.stargazers_count : null;
  } catch {
    return null;
  }
}

function compact(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n);
}

export async function GitHubStars() {
  const count = await stars();
  return (
    <a
      href={githubUrl}
      className="label inline-flex items-center gap-2 px-2 py-1.5 text-muted-foreground transition-colors hover:text-primary"
      aria-label={count === null ? "Cubeship on GitHub" : `Cubeship on GitHub, ${count} stars`}
    >
      <GitHubIcon className="size-4" />
      {count !== null && (
        <span className="inline-flex items-center gap-1">
          <Star className="size-3" />
          {compact(count)}
        </span>
      )}
    </a>
  );
}
