"use client";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type Datastore, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

export default function DatastoreSettingsPage({ params }: PageProps<"/databases/[name]/settings">) {
  const { name } = use(params);
  return <Settings name={name} />;
}

function Settings({ name }: { name: string }) {
  const router = useRouter();
  const path = datastorePath(name);

  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then(setDatastore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  if (error && !datastore) return <ErrorAlert error={error} />;

  return (
    <>
      {" "}
      <ErrorAlert error={error} />
      {/* The header above is drawn from the URL and does not wait — see
          the note on the database's own page. Only what needs the
          answer waits for it. */}
      {!datastore && <LoadingList rows={3} />}
      {datastore && <DatabaseLimits datastore={datastore} onSaved={setDatastore} />}
      {datastore && <ExternalAccess datastore={datastore} onChanged={reload} />}
      {datastore && (
        <>
          <DangerZone>
            <DangerAction
              title="Delete this database"
              description="Stops the container and removes the data directory from the host. There is no backup, and this cannot be undone."
              action={
                <Button variant="destructive" onClick={() => setDeleting(true)}>
                  Delete
                </Button>
              }
            />
          </DangerZone>

          <ConfirmDialog
            open={deleting}
            onOpenChange={setDeleting}
            title={`Delete ${datastore.name}?`}
            description={
              <>
                The container is stopped and the data on disk goes with it. Nothing is kept, and{" "}
                {datastore.attachments.length > 0 ? (
                  <>
                    <strong>{datastore.attachments.map((a) => a.app).join(", ")}</strong> will come
                    up without a connection string the next time they are deployed.
                  </>
                ) : (
                  "no app is attached to it."
                )}
              </>
            }
            confirmWord={datastore.name}
            onConfirm={async () => {
              await api.del(path);
              router.push("/databases");
            }}
          />
        </>
      )}
    </>
  );
}

// Publishing a database on a host port.
//
// Its own section with its own warning rather than a checkbox in the
// general form: this is the difference between a database on a private
// network and a database on the internet, and the thing standing
// between the two is a firewall rule nobody here can write for you.
// DatabaseLimits caps what this database's container may take.
//
// The container on a box this size most worth capping: an app that
// leaks is one app, and a database that takes every page of memory
// takes the daemon and the proxy with it.
function DatabaseLimits({
  datastore,
  onSaved,
}: {
  datastore: Datastore;
  onSaved: (d: Datastore) => void;
}) {
  const [cpu, setCPU] = useState(datastore.limits.cpu ? String(datastore.limits.cpu) : "");
  const [memory, setMemory] = useState(
    datastore.limits.memory_bytes
      ? String(Math.round(datastore.limits.memory_bytes / (1 << 20)))
      : "",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const next = { cpu: Number(cpu) || 0, memory_bytes: (Number(memory) || 0) * (1 << 20) };
  const dirty =
    next.cpu !== datastore.limits.cpu || next.memory_bytes !== datastore.limits.memory_bytes;

  return (
    <>
      <SectionHeader
        title="Limits"
        sub="How much of the machine this database may take. Empty is no limit, which is the default. Changing one takes effect within seconds and does not replace the container the way publishing a port does; removing one waits for the next start."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError(null);
              try {
                onSaved(
                  await api.patch<Datastore>(datastorePath(datastore.name), { limits: next }),
                );
              } catch (err) {
                setError(message(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <ErrorAlert error={error} />

            <div className="space-y-1.5">
              <Label htmlFor="db-cpu">CPU</Label>
              <Input
                id="db-cpu"
                type="number"
                step="0.01"
                min="0"
                placeholder="no limit"
                value={cpu}
                onChange={(e) => setCPU(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Cores, fractional allowed: 0.5 is half a core. A ceiling rather than a share — at
                its limit the container is throttled, not merely preferred less.
              </p>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="db-memory">Memory (MiB)</Label>
              <Input
                id="db-memory"
                type="number"
                min="0"
                placeholder="no limit"
                value={memory}
                onChange={(e) => setMemory(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                A hard ceiling. The kernel enforces it by killing whatever crosses it, so setting
                one below what the database is already holding stops it there and then — which its
                clients see as the connection going away.
              </p>
            </div>

            <Button type="submit" disabled={busy || !dirty}>
              {busy ? "Saving..." : "Save limits"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </>
  );
}

function ExternalAccess({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [unpublishing, setUnpublishing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await fn();
      setPort("");
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const path = datastorePath(datastore.name);

  return (
    <>
      <SectionHeader
        title="External access"
        sub="For something that is not an app on this instance: a migration run from your laptop, psql, a BI tool."
      />

      <ErrorAlert error={error} />

      <Card>
        <CardContent className="space-y-4">
          {datastore.exposed_port ? (
            <>
              <Notice tone="warning">
                Published on{" "}
                <code className="font-mono">
                  {datastore.external_host ?? "this host"}:{datastore.exposed_port}
                </code>
                . There is no TLS in front of it — a database speaks its own protocol on its own
                port — so the password and your firewall are what protect it.
              </Notice>
              <ActionButton variant="outline" busy={busy} onClick={() => setUnpublishing(true)}>
                Stop publishing it
              </ActionButton>
            </>
          ) : (
            <>
              <Notice>Leave empty to take the next free one from 15000-15999.</Notice>
              <div className="space-y-2">
                <Label className="text-xs text-muted-foreground">Host port</Label>
                {/* Joined rather than spaced: the port and the act of
                    publishing on it are one control, and a gap between
                    them invites filling the box and walking away. The
                    negative margin is what makes the two borders share
                    one line. */}
                <div className="flex">
                  <Input
                    value={port}
                    spellCheck={false}
                    placeholder="auto"
                    onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
                    className="h-10 w-40 px-3 text-sm"
                  />
                  <ActionButton
                    variant="outline"
                    busy={busy}
                    className="-ml-px h-10"
                    onClick={() =>
                      run(() => api.post(`${path}/expose`, { port: port ? Number(port) : 0 }))
                    }
                  >
                    Publish it
                  </ActionButton>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Unpublishing replaces the container to drop the port, so
          whatever is connected over it is cut, not merely blocked. */}
      <ConfirmDialog
        open={unpublishing}
        onOpenChange={setUnpublishing}
        title={`Stop publishing ${datastore.name}?`}
        confirmLabel="Stop publishing"
        description="Anything connected over the host port is disconnected, and the container is replaced to drop it. Apps on this instance are unaffected — they reach it by name on the internal network."
        onConfirm={async () => {
          await run(() => api.del(`${path}/expose`));
          setUnpublishing(false);
        }}
      />
    </>
  );
}
