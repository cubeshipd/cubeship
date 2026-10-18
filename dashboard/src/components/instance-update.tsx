"use client";

import { DownloadIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { Notes } from "@/components/release-notes";
import StatusMark from "@/components/status-mark";
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
  const [dismissedFailureAt, setDismissedFailureAt] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Set when this browser started an update. It is what keeps the
  // screen covered through the seconds where the daemon is being
  // replaced and answers nothing at all — the one moment the server
  // cannot tell anybody what is happening.
  const [started, setStarted] = useState(false);
  const observedUpdate = useRef(false);

  const load = useCallback(async () => {
    try {
      const next = await api.get<Updates>("/updates");
      setState(next);
      if (next.run?.status === "running") {
        observedUpdate.current = true;
        setStarted(true);
      }
      if (next.run?.status === "done") {
        setStarted(false);
        if (observedUpdate.current && next.version === next.run.version) {
          observedUpdate.current = false;
          window.location.reload();
        }
      }
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
  const failed = state?.run?.status === "failed";
  const covering = running || (failed && dismissedFailureAt !== state?.run?.started_at);

  useEffect(() => {
    const every = running && state?.run?.status !== "failed" ? RUNNING_POLL : IDLE_POLL;
    const timer = setInterval(load, every);
    return () => clearInterval(timer);
  }, [load, running, state?.run?.status]);

  // Offered once, and then it is the button in the sidebar's business.
  const available = state?.available;
  const offering = !!available && !covering && !dismissed;

  async function start(version: string) {
    setAsking(true);
    setError(null);
    try {
      await api.post("/updates", { version });
      observedUpdate.current = true;
      setStarted(true);
      setDismissed(true);
      await load();
    } catch (err) {
      setError(message(err));
    } finally {
      setAsking(false);
    }
  }

  if (covering)
    return (
      <UpdatingScreen
        run={state?.run}
        onExit={() => {
          setStarted(false);
          setDismissedFailureAt(state?.run?.started_at ?? null);
          setDismissed(true);
        }}
      />
    );

  return (
    <>
      <Dialog open={offering} onOpenChange={(open) => !open && setDismissed(true)}>
        {/* The same column the release-notes dialog is: header and
            footer stay put, and the notes scroll between them. A
            release with a long body otherwise put the button that
            starts the update below the fold. */}
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="shrink-0 border-border border-b p-4 pr-12">
            <DialogTitle>Update to Cubeship {available?.version}</DialogTitle>
            <DialogDescription>
              This instance is on {state?.version}. The update replaces Cubeship across your nodes;
              apps and databases keep running.
            </DialogDescription>
          </DialogHeader>

          <div className="min-w-0 flex-1 overflow-y-auto p-4">
            {available?.notes && <Notes body={available.notes} />}
            <ErrorAlert error={error} />
          </div>

          <DialogFooter className="mx-0 mb-0 shrink-0 rounded-none border-border border-t p-4">
            <Button variant="ghost" onClick={() => setDismissed(true)}>
              Not now
            </Button>
            <ActionButton busy={asking} onClick={() => available && start(available.version)}>
              Update to {available?.version}
            </ActionButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Dismissing the dialog must not be the end of it: an update
          offered once and then never mentioned again is one nobody
          takes. */}
      {available && dismissed && !running && (
        <Button
          variant="outline"
          onClick={() => setDismissed(false)}
          className="fixed right-4 bottom-4 z-40 shadow-lg"
        >
          <DownloadIcon />
          {available.version} available
        </Button>
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
function UpdatingScreen({
  run,
  onExit,
}: {
  run?: import("@/lib/api").UpdateRun;
  onExit: () => void;
}) {
  const failed = run?.status === "failed";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/95 p-4">
      <div className="max-h-[85vh] w-full max-w-lg space-y-6 overflow-y-auto rounded-xl bg-card p-6 ring-1 ring-foreground/10">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold text-foreground">
            {failed
              ? "The update did not finish"
              : `Updating to ${run?.version ?? "a new release"}`}
          </h2>
          <p className="text-muted-foreground text-xs leading-relaxed">
            {failed
              ? "Review the last step, then return to the dashboard."
              : "Apps and databases keep running. The dashboard may briefly disconnect."}
          </p>
        </div>

        <ol className="space-y-3" aria-label="Update progress">
          {(run?.done ?? []).map((step, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: completed steps only append and can repeat while a worker reconnects
            <li key={`${index}:${step}`} className="min-w-0 break-words">
              <StatusMark status="done" label={step} size={20} fontSize={13} strike={false} />
            </li>
          ))}
          {run?.step && (
            <li className="min-w-0 break-words">
              <StatusMark
                status={failed ? "failed" : "running"}
                label={run.step}
                size={20}
                fontSize={13}
                strike={false}
              />
            </li>
          )}
          {!run?.step && !failed && (
            <li>
              <StatusMark status="running" label="Starting update…" size={20} fontSize={13} />
            </li>
          )}
        </ol>

        <ErrorAlert error={run?.error ?? null} />
        {failed && (
          <Button onClick={onExit} className="w-full">
            Back to dashboard
          </Button>
        )}
      </div>
    </div>
  );
}
