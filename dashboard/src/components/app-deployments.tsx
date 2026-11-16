"use client";

import { GitBranchIcon, GitCommitHorizontalIcon, RotateCcwIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { LogView } from "@/components/container-logs";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, type Deployment } from "@/lib/api";
import { message } from "@/lib/errors";

// The one reason a deploy's record cannot be removed: the daemon is
// still writing to it.
const stillRunning = "This deploy is still running. Wait for it to finish.";

// Why a row's rollback is off, or what it does when it is on.
function rollbackTitle(d: Deployment): string {
  if (d.restorable) {
    return d.git_sha ? `Deploy commit ${d.git_sha.slice(0, 7)} again.` : `Deploy ${d.image} again.`;
  }
  if (d.live) return "The app is already running this deploy.";
  if (d.status !== "succeeded") return "Only a deploy that succeeded can be rolled back to.";
  return "This deploy's version cannot be named by the app's current source.";
}

// Deploying from Actions, with nothing to fill in.
//
// **No tag field.** What a deploy runs is the app's source and the ref
// it is configured with — a branch for something built here, a tag for
// an image — and that is a decision with consequences, made in the
// app's settings where it is written down and stays written down.
// Typing a branch beside the button made every deploy a fresh decision
// about which code this app is, taken in the moment and remembered
// nowhere.
export function Deployments({
  reference,
  deployed,
  canDeploy,
  onSettled,
}: {
  reference: string;
  canDeploy: boolean;
  // Bumped when the header deploys, which is the one thing that changes
  // this list from outside it.
  deployed: number;
  onSettled: () => void;
}) {
  const [list, setList] = useState<Deployment[] | null>(null);
  const [open, setOpen] = useState<Deployment | null>(null);
  const [deleting, setDeleting] = useState<Deployment | null>(null);
  const [restoring, setRestoring] = useState<Deployment | null>(null);
  // The app's own name, which is what the confirmation asks somebody to
  // type before it takes the app off the air.
  const name = reference.split("/").pop() ?? reference;
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/deployments`;
  const reload = useCallback(() => {
    api
      .get<Deployment[]>(path)
      .then(setList)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // A bump from Deploy tells this list there is a new row to fetch.
  useEffect(() => {
    if (deployed === 0) return;
    reload();
  }, [deployed, reload]);

  // A deploy runs detached, so the row is the only place its outcome
  // appears. Poll while one is in flight and stop when none is.
  useEffect(() => {
    if (!list?.some((d) => d.status === "pending" || d.status === "deploying")) return;
    const timer = setInterval(() => {
      reload();
      onSettled();
    }, 2000);
    return () => clearInterval(timer);
  }, [list, reload, onSettled]);

  const columns: Column<Deployment>[] = [
    {
      id: "status",
      header: "Status",
      width: 16,
      sortBy: (d) => d.status,
      // A stalled deploy is still `pending` — that is what lets a
      // machine that comes back finish the rollout it missed — so the
      // badge is the truth and the sentence beside it is the rest of
      // it: pending on its own reads as "happening", and this one is
      // not happening.
      cell: (d) => (
        <div className="space-y-1">
          <StatusBadge value={d.status} />
          {d.stalled_on && d.stalled_on.length > 0 && (
            <div className="font-mono text-[10px] text-warning">
              waiting on {d.stalled_on.join(", ")}
            </div>
          )}
        </div>
      ),
    },
    {
      id: "commit",
      header: "Commit / Image",
      width: 56,
      wrap: true,
      cell: (d) =>
        d.git_sha ? (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1.5" title={d.git_sha}>
              <GitCommitHorizontalIcon className="size-3.5" />
              {d.git_sha.slice(0, 7)}
            </span>
            {d.git_branch && (
              <span className="inline-flex min-w-0 items-center gap-1.5">
                <GitBranchIcon className="size-3.5 shrink-0" />
                <span className="truncate" title={d.git_branch}>
                  {d.git_branch}
                </span>
              </span>
            )}
          </div>
        ) : (
          <div className="font-mono text-xs break-all text-muted-foreground">{d.image || "—"}</div>
        ),
    },
    {
      id: "when",
      header: "When",
      width: 20,
      sortBy: (d) => d.created_at,
      cell: (d) => (
        <span className="text-xs text-muted-foreground">
          {new Date(d.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 8,
      align: "right",
      // Disabled with the reason rather than hidden: a missing button
      // explains nothing, and "why can I not clear this one" is the
      // question somebody has while looking straight at it.
      cell: (d) => (
        <RowActions>
          <RowAction
            icon={RotateCcwIcon}
            label={`Roll back to deploy ${d.id}`}
            disabled={!canDeploy || !d.restorable}
            title={!canDeploy ? "You cannot deploy this app." : rollbackTitle(d)}
            onClick={() => setRestoring(d)}
          />
          <RowAction
            icon={Trash2Icon}
            label={d.live ? `Take the app down` : `Delete deploy ${d.id}`}
            danger={d.deletable}
            disabled={!d.deletable}
            title={
              d.deletable
                ? d.live
                  ? "This is what the app is running. Deleting it takes the app down."
                  : d.stalled_on && d.stalled_on.length > 0
                    ? `Nothing is coming for this: ${d.stalled_on.join(", ")} stopped answering. Deleting it means the machine that comes back runs the deploy below this one.`
                    : undefined
                : stillRunning
            }
            onClick={() => setDeleting(d)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader title="Deployments" />
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={list}
        rowKey={(d) => String(d.id)}
        loadingRows={3}
        maxHeight="50vh"
        empty="Nothing deployed yet."
        // Every row opens, not only the ones that built something. What
        // is behind it is the deploy — what ran, when, why it stopped
        // and what it printed — and a row that opened for some deploys
        // and not others would be a table you have to guess at.
        onRowClick={setOpen}
      />

      <DeploymentDialog
        reference={reference}
        deployment={open}
        onOpenChange={(shown) => !shown && setOpen(null)}
      />

      <ConfirmDialog
        open={restoring !== null}
        onOpenChange={(shown) => !shown && setRestoring(null)}
        title={`Roll back to deploy ${restoring?.id ?? ""}?`}
        confirmLabel="Roll back"
        description={
          <>
            Deploys{" "}
            <code className="break-all text-foreground">
              {restoring?.git_sha ? restoring.git_sha.slice(0, 7) : restoring?.image}
            </code>{" "}
            again, with the app's current environment. What is running now keeps serving until the
            new container passes its health check.
          </>
        }
        onConfirm={async () => {
          if (!restoring) return;
          const started = await api.post<Deployment>(`${path}/${restoring.id}/rollback`, {});
          setRestoring(null);
          reload();
          onSettled();
          setOpen(started);
        }}
      />

      {/* Holding confirms removal of the app record or stopping a live app. */}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(shown) => !shown && setDeleting(null)}
        title={deleting?.live ? "Take the app down?" : "Delete this record?"}
        holdToConfirm
        confirmLabel={deleting?.live ? "Take it down" : "Delete"}
        description={
          deleting?.live ? (
            <>
              This is what <code className="text-foreground">{name}</code> is running. Its container
              is stopped and removed, and the app stops answering.{" "}
              <strong>The app itself stays</strong> — its domains, its environment and everything
              attached to it — and the next deploy brings it back.
            </>
          ) : (
            <>
              The row goes, and the build log with it. <strong>The app is not touched</strong> —
              this is the history of a deploy, not anything running now.
            </>
          )
        }
        onConfirm={async () => {
          if (!deleting) return;
          await api.del(`${path}/${deleting.id}`);
          setDeleting(null);
          reload();
          onSettled();
        }}
      />
    </>
  );
}

// One deploy, opened.
//
// It fetches the deployment again rather than showing the row it was
// opened from, because the row does not carry the log: a build's output
// is capped at 256 KiB and the listing is fifty rows, so the daemon
// hands it over one deployment at a time. While the deploy is still
// running it asks again on a timer, which is what makes this the place
// to watch a build from.
function DeploymentDialog({
  reference,
  deployment,
  onOpenChange,
}: {
  reference: string;
  // The row that was clicked, or null when nothing is open.
  deployment: Deployment | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [full, setFull] = useState<Deployment | null>(null);
  const [error, setError] = useState<string | null>(null);

  const id = deployment?.id;
  const load = useCallback(() => {
    if (!id) return;
    api
      .get<Deployment>(`/apps/${reference}/deployments/${id}`)
      .then((d) => {
        setFull(d);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, [reference, id]);

  useEffect(() => {
    setFull(null);
    load();
  }, [load]);

  // A build writes its output as it goes, so this is the one screen
  // where re-reading under somebody is the point rather than a
  // discourtesy — and it stops the moment the deploy settles.
  const running = (full ?? deployment)?.status === "pending";
  useEffect(() => {
    if (!id || !running) return;
    const timer = setInterval(load, 2000);
    return () => clearInterval(timer);
  }, [id, running, load]);

  const shown = full ?? deployment;
  const buildLogs = full
    ? [
        full.logs?.trimEnd(),
        full.error && !full.logs?.includes(full.error)
          ? `\u001b[31mDeployment error: ${full.error}\u001b[0m`
          : "",
      ]
        .filter(Boolean)
        .join("\n\n") || null
    : null;

  // Focus lands on the panel rather than on the first thing in it.
  // The first thing in it is the log's filter, and a dialog opened to
  // read a build's output should not open with a cursor blinking in a
  // field: it is what a dialog that wants typing looks like, and here
  // there is nothing to type until somebody has read something.
  const panel = useRef<HTMLDivElement>(null);

  return (
    <Dialog open={deployment !== null} onOpenChange={onOpenChange}>
      <DialogContent ref={panel} initialFocus={panel} className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-3">
            Deploy
            {shown && <StatusBadge value={shown.status} />}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-4">
          <ErrorAlert error={error} />

          {shown && (
            <div className="flex flex-wrap items-center gap-x-6 gap-y-1 font-mono text-xs text-muted-foreground">
              <span className="break-all">{shown.image || "—"}</span>
              <span>{shown.phase}</span>
              <span>{Math.round(shown.duration_ms / 1000)}s</span>
              <span>{new Date(shown.created_at).toLocaleString()}</span>
            </div>
          )}

          {shown && !shown.cancelled && shown.status === "pending" && (
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                await api.post(`/apps/${reference}/deployments/${shown.id}/cancel`);
                await load();
              }}
            >
              Cancel deploy
            </Button>
          )}

          <LogView
            text={buildLogs}
            busy={full === null}
            name={`${reference.split("/").pop()}-deploy-${deployment?.id ?? ""}`}
            follow={running}
            empty={
              running
                ? "Waiting for the build to say something."
                : "This deploy printed nothing — it ran an image somebody else built rather than building one."
            }
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}
