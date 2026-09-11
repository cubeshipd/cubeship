"use client";

import {
  ClockIcon,
  CloudIcon,
  DownloadIcon,
  RotateCcwIcon,
  ServerIcon,
  Trash2Icon,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  api,
  type Backup,
  type BackupSchedule,
  type Bucket,
  datastorePath,
  formatBytes,
  type ObjectStore,
} from "@/lib/api";
import { message } from "@/lib/errors";

// when is the moment a backup was taken, in the reader's own locale. A
// backup is read against "was there one last night", which is a
// question about their clock rather than the server's.
function when(value: string): string {
  if (!value) return "";
  return new Date(value).toLocaleString();
}

// A database's backups: when they are taken, what there is, and putting
// one back.
//
// **The one fact every row leads with is whether it is off this
// machine.** A dump beside the database it came from survives somebody
// dropping a table and nothing else — not the disk, not the box, not
// the provider — and a screen that showed it as simply "a backup" would
// be the only thing on the instance claiming otherwise.

// POLL is how often a running dump is re-read. A backup is detached, so
// the row is the only thing that knows how it went; a dump of anything
// worth taking is minutes, and a second is fast enough to watch.
const POLL = 2000;

export function Backups({
  database,
  canBackUp,
  consistency,
  hasContainer,
}: {
  database: string;
  canBackUp: boolean;
  consistency?: string;
  hasContainer: boolean;
}) {
  const [rows, setRows] = useState<Backup[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const path = `${datastorePath(database)}/backups`;
  const load = useCallback(() => {
    api
      .get<Backup[]>(path)
      .then(setRows)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(load, [load]);

  // While one is running the row is the only place its outcome lands,
  // so the list re-reads itself until nothing is.
  const running = (rows ?? []).some((b) => b.status === "taking");
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(load, POLL);
    return () => clearInterval(timer);
  }, [running, load]);

  if (!canBackUp) {
    return (
      <Notice>
        This engine is not backed up here. Taking a dump would be easy and putting one back is not:
        its file is read once, when the server starts, so a restore means stopping the database.
      </Notice>
    );
  }

  return (
    <>
      <Schedule database={database} onChanged={load} />

      <SectionHeader
        title="Backups"
        sub={consistency}
        actions={
          <ActionButton
            busy={busy}
            disabled={!hasContainer}
            title={hasContainer ? undefined : "This database has no container to dump."}
            onClick={async () => {
              setBusy(true);
              setError(null);
              try {
                await api.post(path, {});
                load();
              } catch (e) {
                setError(message(e));
              }
              setBusy(false);
            }}
          >
            Back up now
          </ActionButton>
        }
      />
      <ErrorAlert error={error} />
      <BackupTable rows={rows} onChanged={load} showDatabase={false} />
    </>
  );
}

// BackupTable is the dumps themselves: a database's own tab, and the
// listing of the ones whose database has been deleted. They differ in
// one column and nothing else.
//
// The instance-wide screen is **not** one of these any more. A list of
// every dump on the box is a log, and the question people open that
// screen with — am I covered — is one a list of backups cannot answer:
// a database nobody has ever dumped is in no such list. See
// `/backups`.
export function BackupTable({
  rows,
  onChanged,
  showDatabase,
}: {
  rows: Backup[] | null;
  onChanged: () => void;
  showDatabase: boolean;
}) {
  const [restoring, setRestoring] = useState<Backup | null>(null);
  const [deleting, setDeleting] = useState<Backup | null>(null);
  const [error, setError] = useState<string | null>(null);

  const columns: Column<Backup>[] = [
    ...(showDatabase
      ? [
          {
            id: "database",
            header: "Database",
            width: 20,
            sortBy: (b: Backup) => b.database,
            // No "(deleted)" marker any more. This column is only ever
            // shown on the orphan listing, where every row's database
            // is gone and the heading above says so — so the marker was
            // on every row, saying nothing, and truncating the one
            // thing the column is for.
            cell: (b: Backup) => <span className="truncate font-mono">{b.database}</span>,
          },
        ]
      : []),
    {
      id: "taken",
      header: "Taken",
      width: showDatabase ? 22 : 30,
      sortBy: (b: Backup) => b.started_at,
      cell: (b: Backup) => (
        <span title={b.started_at}>
          {when(b.started_at)}
          {b.scheduled && <span className="ml-2 text-muted-foreground">scheduled</span>}
        </span>
      ),
    },
    {
      id: "where",
      header: "Where",
      width: showDatabase ? 22 : 28,
      cell: (b: Backup) =>
        b.off_machine ? (
          <span className="font-mono text-xs">
            {b.store}/{b.bucket}
          </span>
        ) : (
          // Said on every row rather than once above the table: this is
          // the difference between a copy and a backup, and a row that
          // did not say it would be the screen claiming otherwise.
          <span className="text-warning">on this machine</span>
        ),
    },
    {
      id: "size",
      header: "Size",
      width: 12,
      align: "right" as const,
      sortBy: (b: Backup) => b.size_bytes,
      cell: (b: Backup) => (b.size_bytes > 0 ? formatBytes(b.size_bytes) : "—"),
    },
    {
      id: "status",
      header: "Status",
      width: showDatabase ? 12 : 16,
      cell: (b: Backup) => (
        <span title={b.error}>
          <StatusBadge value={b.status} />
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 14,
      align: "right" as const,
      cell: (b: Backup) => (
        <RowActions>
          {/* A dump still being taken is one whose object is still being
              written, and half of one restores into a database that is
              half replaced. */}
          <RowAction
            icon={DownloadIcon}
            label="Download"
            disabled={b.status === "taking"}
            onClick={() => window.location.assign(`/api/backups/${b.id}/download`)}
          />
          <RowAction
            icon={RotateCcwIcon}
            label="Restore"
            disabled={b.status !== "succeeded" || !b.database_exists}
            title={
              b.database_exists
                ? undefined
                : "The database this came from has been deleted, and choosing where to put it is not something this release does."
            }
            onClick={() => setRestoring(b)}
          />
          <RowAction
            icon={Trash2Icon}
            label="Delete"
            danger
            disabled={b.status === "taking"}
            onClick={() => setDeleting(b)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <ErrorAlert error={error} />
      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(b) => String(b.id)}
        empty="No backups yet."
      />

      <ConfirmDialog
        open={restoring !== null}
        onOpenChange={(open) => !open && setRestoring(null)}
        title={`Restore ${restoring?.database} from ${when(restoring?.started_at ?? "")}?`}
        description={
          <>
            <strong>Everything in the database now is replaced</strong> by what was in it when this
            was taken, and that cannot be undone.
            <br />
            <br />
            The database is not stopped while it happens, so an app writing during the restore
            leaves a state that is neither the backup nor what was there. Stop what writes to it
            first if you can.
          </>
        }
        confirmWord={restoring?.database}
        confirmLabel="Restore"
        onConfirm={async () => {
          setError(null);
          try {
            await api.post(`/backups/${restoring?.id}/restore`, {});
            setRestoring(null);
            onChanged();
          } catch (e) {
            setError(message(e));
            throw e;
          }
        }}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete this backup?"
        description="The dump goes with it, wherever it is. Nothing else has a copy."
        onConfirm={async () => {
          setError(null);
          try {
            await api.del(`/backups/${deleting?.id}`);
            setDeleting(null);
            onChanged();
          } catch (e) {
            setError(message(e));
            throw e;
          }
        }}
      />
    </>
  );
}

// Schedule is the half nobody watches: when, where, and how many to
// keep. Off is the absence of a schedule, which is why the switch is
// what creates and removes one rather than a field on it.
function Schedule({ database, onChanged }: { database: string; onChanged: () => void }) {
  const [schedule, setSchedule] = useState<BackupSchedule | null>(null);
  const [on, setOn] = useState(false);
  const [at, setAt] = useState("03:00");
  const [timezone, setTimezone] = useState(
    // The browser's, because 03:00 on a server's clock is not the middle
    // of anybody's night and this is the only place that knows which
    // night somebody means.
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [keep, setKeep] = useState("7");
  const [storeID, setStoreID] = useState("");
  const [bucket, setBucket] = useState("");
  const [stores, setStores] = useState<ObjectStore[]>([]);
  const [buckets, setBuckets] = useState<Bucket[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `${datastorePath(database)}/backups/schedule`;

  useEffect(() => {
    api
      .get<BackupSchedule>(path)
      .then((s) => {
        setSchedule(s);
        setOn(true);
        setAt(s.at);
        setTimezone(s.timezone);
        setKeep(String(s.keep));
        setStoreID(s.store ?? "");
        setBucket(s.bucket ?? "");
      })
      // 404 is what "not scheduled" is, rather than an error: the row
      // existing is the whole of it.
      .catch(() => setSchedule(null));
  }, [path]);

  useEffect(() => {
    api
      .get<ObjectStore[]>("/objectstores")
      .then(setStores)
      .catch(() => setStores([]));
  }, []);

  // The buckets of whichever store is chosen, so the destination is
  // picked rather than typed — and a name that does not exist is a
  // schedule that fails nightly.
  useEffect(() => {
    if (!storeID) {
      setBuckets(null);
      return;
    }
    const store = stores.find((s) => String(s.name) === storeID);
    if (!store) return;
    let live = true;
    setBuckets(null);
    api
      .get<Bucket[]>(`/objectstores/${encodeURIComponent(store.name)}/buckets`)
      .then((b) => live && setBuckets(b))
      .catch(() => live && setBuckets([]));
    return () => {
      live = false;
    };
  }, [storeID, stores]);

  const chosen = stores.find((s) => String(s.name) === storeID);

  async function save() {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      if (!on) {
        await api.del(path);
        setSchedule(null);
      } else {
        const next = await api.put<BackupSchedule>(path, {
          at,
          timezone,
          keep: Number(keep) || 0,
          store: chosen ? chosen.name : "",
          bucket: chosen ? bucket : "",
        });
        setSchedule(next);
      }
      setSaved(true);
      onChanged();
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }

  return (
    <>
      <SectionHeader title="On a schedule" />
      <Card className="mb-6">
        <CardContent className="space-y-5">
          <ErrorAlert error={error} />

          <div className="flex items-start gap-3">
            <Switch
              id="scheduled"
              checked={on}
              onCheckedChange={(v) => {
                setOn(v);
                setSaved(false);
              }}
            />
            <div className="space-y-1">
              <Label htmlFor="scheduled">
                <ClockIcon className="mr-1 inline size-3.5" />
                Back this database up every day
              </Label>
              <p className="max-w-prose text-xs text-muted-foreground">
                A time of day rather than an interval, because what you are choosing is when the
                database may be busy. A window this instance was down for runs late rather than
                being skipped.
                {schedule?.last_run_at && ` Last run ${when(schedule.last_run_at)}.`}
              </p>
            </div>
          </div>

          {on && (
            <div className="space-y-5 border-l-2 border-primary/40 pl-4">
              <div className="grid gap-4 sm:grid-cols-[8rem_1fr_8rem]">
                <TextField
                  label="At"
                  value={at}
                  spellCheck={false}
                  placeholder="03:00"
                  onChange={(e) => setAt(e.target.value)}
                />
                <TextField
                  label="Timezone"
                  value={timezone}
                  spellCheck={false}
                  onChange={(e) => setTimezone(e.target.value)}
                  hint="An IANA name, like America/Bahia."
                />
                <TextField
                  label="Keep"
                  value={keep}
                  spellCheck={false}
                  onChange={(e) => setKeep(e.target.value)}
                  hint="0 keeps every one."
                />
              </div>

              {/* **The destinations are S3, and the list says which of
                  them is actually somewhere else.** A managed store is
                  a MinIO container this instance runs, with its objects
                  on this same disk — so it is grouped with the local
                  disk by its icon rather than with the endpoints
                  elsewhere, and the hint names the provider a linked
                  one speaks. Reading the list, the two that are not a
                  backup look like each other. */}
              <SearchableSelect
                label="Where they go"
                searchable={stores.length > 8}
                value={storeID}
                onChange={(v) => {
                  setStoreID(v);
                  setBucket("");
                }}
                choices={[
                  {
                    value: "",
                    label: "This machine's disk",
                    icon: ServerIcon,
                    hint: "not a backup",
                  },
                  ...stores.map((s) => ({
                    value: s.name,
                    label: s.name,
                    icon: s.kind === "managed" ? ServerIcon : CloudIcon,
                    hint:
                      s.kind === "managed"
                        ? `${s.provider_label} on this machine`
                        : s.provider_label,
                  })),
                ]}
                hint="An S3 bucket, on a provider this instance is connected to. Somewhere else is the whole point: a dump on this disk goes with the machine."
              />

              {chosen && (
                <SearchableSelect
                  label="Bucket"
                  value={bucket}
                  busy={buckets === null}
                  onChange={setBucket}
                  choices={(buckets ?? []).map((b) => ({ value: b.name, label: b.name }))}
                  empty="That store holds no buckets yet."
                />
              )}

              {/* **The same warning for two destinations, because they
                  are the same disk.** A managed store is a MinIO this
                  instance runs, with its objects in a bind mount under
                  the data directory — so picking one is not sending the
                  dumps anywhere, and it used to read as though it were.
                  Only a linked store is somewhere else. */}
              {!chosen && (
                <Notice tone="warning">
                  With no store chosen these land beside the database, on this machine&rsquo;s own
                  disk. That survives somebody dropping a table and nothing else — not the disk, not
                  the box. Link an S3 bucket under Object storage and pick it here.
                </Notice>
              )}
              {chosen?.kind === "managed" && (
                <Notice tone="warning">
                  <strong>{chosen.name} runs on this machine.</strong> It is a MinIO this instance
                  started, and its objects sit on the same disk as the database — so these dumps go
                  no further than a local one does. Link an S3 bucket somewhere else to have a
                  backup that survives the box.
                </Notice>
              )}
            </div>
          )}

          <div className="flex items-center gap-3">
            <ActionButton busy={busy} onClick={save}>
              Save
            </ActionButton>
            {saved && <span className="text-xs text-success">Saved.</span>}
            {on && !schedule && (
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setOn(false);
                  setSaved(false);
                }}
              >
                Cancel
              </Button>
            )}
          </div>
        </CardContent>
      </Card>
    </>
  );
}
