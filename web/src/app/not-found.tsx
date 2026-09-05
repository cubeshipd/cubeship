import Link from "next/link";

// The 404 every unmatched route lands on, including a project or an app
// whose slug no longer exists — a link someone was sent, or a bookmark
// from before something was deleted.
//
// Not styled as a page inside the dashboard: the shell needs an account
// and a sidebar, and neither is guaranteed here.
export default function NotFound() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background bg-grid px-6">
      <div className="hud-frame w-full max-w-md bg-card p-8">
        <p className="font-mono text-[11px] tracking-[0.2em] text-primary uppercase">404</p>
        <h1 className="mt-3 text-xl font-semibold">Nothing at this address</h1>
        <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
          The page, project or app this URL names does not exist on this instance — or it did, and
          has since been deleted.
        </p>
        <Link
          href="/"
          className="mt-6 inline-block font-mono text-xs text-primary underline underline-offset-4"
        >
          Back to projects
        </Link>
      </div>
    </main>
  );
}
