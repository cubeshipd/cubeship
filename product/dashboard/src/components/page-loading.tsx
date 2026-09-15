import { Wordmark } from "@/components/brand";
import { LoadingList } from "@/components/loading";
import { Button } from "@/components/ui/button";

// Loading frames preserve the content column while the shared navigation
// remains available. No counts or status colours stand in for missing data.
export function PageLoading({
  kind = "page",
  label = "Loading page",
}: {
  kind?: "page" | "grid" | "form";
  label?: string;
}) {
  return (
    <div className="dashboard-loading" role="status" aria-label={label}>
      <span className="sr-only">{label}</span>
      <div aria-hidden="true">
        <div className="dashboard-loading-heading">
          <span className="scan-block h-5 w-40" />
          <span className="scan-block h-8 w-24" />
        </div>
        {kind === "form" ? (
          <div className="dashboard-loading-form">
            {["first", "second", "third"].map((field) => (
              <div key={field} className="space-y-3">
                <span className="scan-block block h-3 w-28" />
                <span className="block h-10 border border-border bg-background" />
                <span className="scan-line block w-2/3" />
              </div>
            ))}
          </div>
        ) : (
          <>
            <div className="dashboard-loading-grid">
              {["first", "second", "third"].map((card) => (
                <div key={card} className="dashboard-loading-card">
                  <span className="scan-block block size-10" />
                  <div className="flex-1 space-y-3">
                    <span className="scan-block block h-3 w-3/4" />
                    <span className="scan-line block w-1/2" />
                  </div>
                  <div className="dashboard-loading-card-footer">
                    <span className="scan-line block w-full" />
                  </div>
                </div>
              ))}
            </div>
            {kind === "page" && <LoadingList rows={4} />}
          </>
        )}
      </div>
    </div>
  );
}

export function SessionLoading({
  error,
  onRetry,
}: {
  error?: string | null;
  onRetry?: () => void;
}) {
  return (
    <div className="dashboard-shell dashboard-session-loading">
      <aside className="dashboard-sidebar" aria-hidden="true">
        <div className="dashboard-brand">
          <Wordmark className="text-sm" markClassName="size-6" />
        </div>
        <div className="space-y-7 p-7">
          {["overview", "projects", "data", "storage", "platform"].map((item) => (
            <span key={item} className="scan-line block w-3/4" />
          ))}
        </div>
      </aside>
      <main className="dashboard-main">
        <div className="dashboard-header">
          <div className="dashboard-container dashboard-header-inner">
            <span className="text-sm text-muted-foreground">Opening your workspace</span>
          </div>
        </div>
        <div className="dashboard-container dashboard-content">
          {error ? (
            <div className="dashboard-loading-error" role="alert">
              <h1>We couldn’t reach your instance.</h1>
              <p>{error}</p>
              <Button onClick={onRetry}>Try again</Button>
            </div>
          ) : (
            <PageLoading label="Loading your session" />
          )}
        </div>
      </main>
    </div>
  );
}
