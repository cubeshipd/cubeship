"use client";

import { DownloadIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type Updates } from "@/lib/api";
import { message } from "@/lib/errors";

// How often the instance is asked about updates while nothing is
// happening, and while something is.
//
// The slow one is a release lookup, and releases do not appear by the
// minute. The fast one is a progress step, and it is what somebody is
// watching.
const IDLE_POLL = 60 * 60 * 1000;
const RUNNING_POLL = 2000;

// InstanceUpdate is the whole of updating from the dashboard: the
// dialog that offers it, the button that stays behind when the dialog
// is dismissed, and the screen that covers everything while it runs.
//
// **It renders over the shell rather than on a settings page**, because
// two of its three jobs are not things somebody navigates to: an update
// offered once and then never mentioned again is one nobody takes, and
// a lock that only exists on one page is not a lock.
export function InstanceUpdate() {
  const [state, setState] = useState<Updates | null>(null);
  const [asking, setAsking] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Set when this browser started an update. It is what keeps the
  // screen covered through the seconds where the daemon is being
  // replaced and answers nothing at all — the one moment the server
  // cannot tell anybody what is happening.
  const [started, setStarted] = useState(false);

  const load = useCallback(async () => {
    try {
      const next = await api.get<Updates>("/updates");
      setState(next);
      if (next.run?.status !== "running") setStarted(false);
      return next;
    } catch {
      // An instance mid-restart answers nothing. Keeping the last
      // answer is what makes the cover stay up across it.
      return null;
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const running = state?.run?.status === "running" || started;

  useEffect(() => {
    const every = running ? RUNNING_POLL : IDLE_POLL;
    const timer = setInterval(load, every);
    return () => clearInterval(timer);
  }, [load, running]);

  // Offered once, and then it is the button in the sidebar's business.
  const available = state?.available;
  const offering = !!available && !running && !dismissed;

  async function start(version: string) {
    setAsking(true);
    setError(null);
    try {
      await api.post("/updates", { version });
      setStarted(true);
      setDismissed(true);
      await load();
    } catch (err) {
      setError(message(err));
    } finally {
      setAsking(false);
    }
  }

  if (running) return <UpdatingScreen run={state?.run} />;

  return (
    <>
      <Dialog open={offering} onOpenChange={(open) => !open && setDismissed(true)}>
        <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Cubeship {available?.version} is out</DialogTitle>
            <DialogDescription>
              This instance is on {state?.version}. Updating replaces the daemon, the dashboard and
              every other machine in this cluster — your apps and databases keep running, and
              nothing can be changed here until it finishes.
            </DialogDescription>
          </DialogHeader>

          {available?.notes && (
            <pre className="max-h-64 overflow-y-auto whitespace-pre-wrap border border-border p-3 font-mono text-[11px] text-muted-foreground">
              {available.notes}
            </pre>
          )}
          {error && <p className="font-mono text-destructive text-xs">{error}</p>}

          <DialogFooter>
            <Button variant="ghost" onClick={() => setDismissed(true)}>
              Not now
            </Button>
            <Button disabled={asking} onClick={() => available && start(available.version)}>
              {asking ? "Starting..." : `Update to ${available?.version}`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Dismissing the dialog must not be the end of it: an update
          offered once and then never mentioned again is one nobody
          takes. */}
      {available && dismissed && !running && (
        <button
          type="button"
          onClick={() => setDismissed(false)}
          className="fixed right-4 bottom-4 z-40 flex items-center gap-2 border border-primary/40 bg-card px-3 py-2 font-mono text-[11px] text-primary transition-colors hover:bg-primary/10"
        >
          <DownloadIcon className="size-3.5" />
          {available.version} available
        </button>
      )}
    </>
  );
}

// UpdatingScreen covers the dashboard while the instance replaces
// itself.
//
// **It is a cover, not a disabled state.** The daemon refuses every
// write for the same span, so this is not what makes the instance safe
// — it is what stops somebody spending a minute filling in a form that
// was always going to be refused.
function UpdatingScreen({ run }: { run?: import("@/lib/api").UpdateRun }) {
  const failed = run?.status === "failed";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/95 p-8">
      <div className="w-full max-w-lg space-y-5 border border-border bg-card p-6">
        <div className="space-y-1">
          <h2 className="font-medium text-foreground text-sm uppercase tracking-wide">
            {failed
              ? "The update did not finish"
              : `Updating to ${run?.version ?? "a new release"}`}
          </h2>
          <p className="text-muted-foreground text-xs leading-relaxed">
            {failed
              ? "Nothing else was changed. What follows is as far as it got."
              : "Your apps and databases keep running. This dashboard comes back on its own — it may go quiet for a few seconds while the daemon is replaced."}
          </p>
        </div>

        <ol className="space-y-1.5 font-mono text-[11px]">
          {(run?.done ?? []).map((step) => (
            <li key={step} className="text-muted-foreground">
              <span className="text-primary">✓</span> {step}
            </li>
          ))}
          {run?.step && (
            <li className="text-foreground">
              <span className="animate-pulse text-primary">▸</span> {run.step}
            </li>
          )}
          {!run && <li className="text-muted-foreground">Starting…</li>}
        </ol>

        {run?.error && (
          <p className="border border-destructive/40 p-3 font-mono text-[11px] text-destructive">
            {run.error}
          </p>
        )}
        {failed && (
          <Button onClick={() => window.location.reload()} className="w-full">
            Reload
          </Button>
        )}
      </div>
    </div>
  );
}
