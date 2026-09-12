import { Star } from "lucide-react";
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
      <svg viewBox="0 0 24 24" aria-hidden="true" className="size-4 fill-current">
        <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2.17c-3.2.7-3.87-1.37-3.87-1.37-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.7 1.25 3.36.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.78 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.8 1.19 1.83 1.19 3.09 0 4.42-2.7 5.39-5.26 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z" />
      </svg>
      {count !== null && (
        <span className="inline-flex items-center gap-1">
          <Star className="size-3" />
          {compact(count)}
        </span>
      )}
    </a>
  );
}
