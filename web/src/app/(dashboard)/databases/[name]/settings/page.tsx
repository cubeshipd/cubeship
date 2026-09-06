"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextAreaField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { api, type Datastore, datastoreLabel, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

export default function DatastoreSettingsPage({ params }: PageProps<"/databases/[name]/settings">) {
  const { name } = use(params);
  return <Settings name={name} />;
}

function Settings({ name }: { name: string }) {
  const router = useRouter();
  const path = datastorePath(name);

  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then((d) => {
        setDatastore(d);
        setDescription(d.description ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      setDatastore(await api.patch<Datastore>(path, { description }));
      setSaved(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  if (error && !datastore) return <ErrorAlert error={error} />;
  if (!datastore) return null;

  const dirty = description !== (datastore.description ?? "");

  return (
    <>
      <Link
        href={`/databases/${name}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {name}
      </Link>

      <PageHeader
        title="Database settings"
        sub="What this database is for, who outside the instance can reach it, and how to get rid of it."
      />

      <ErrorAlert error={error} />

      <SectionHeader title="General" />
      <Card>
        <CardContent>
          <form onSubmit={save} className="space-y-4">
            <TextAreaField
              label="Description"
              value={description}
              onChange={(e) => {
                setDescription(e.target.value);
                setSaved(false);
              }}
              hint="What this database holds. With no project above it to say where it belongs, this is the only place that can."
            />

            {/* Everything else about a database is fixed, and each for
                its own reason. Saying so beats a form that silently
                ignores what you typed into it. */}
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Fixed after creation</Label>
              <Table>
                <TableBody>
                  <TableRow>
                    <TableCell className="w-32 text-muted-foreground">Name</TableCell>
                    <TableCell className="font-mono">{datastore.name}</TableCell>
                    <TableCell className="text-xs whitespace-normal text-subtle-foreground">
                      It is the container's own name, which every attached app resolves.
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell className="text-muted-foreground">Engine</TableCell>
                    <TableCell className="font-mono">
                      {datastoreLabel(datastore.engine)} {datastore.version}
                    </TableCell>
                    <TableCell className="text-xs whitespace-normal text-subtle-foreground">
                      A data directory written by one major version cannot be read by another.
                      Changing version means a second database and a migration.
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell className="text-muted-foreground">Username</TableCell>
                    <TableCell className="font-mono">{datastore.username}</TableCell>
                    <TableCell className="text-xs whitespace-normal text-subtle-foreground">
                      The login the server was created with. Others are made inside the database.
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>

            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty}>
                Save
              </ActionButton>
              {saved && !dirty && <span className="text-xs text-muted-foreground">Saved.</span>}
            </div>
          </form>
        </CardContent>
      </Card>

      <ExternalAccess datastore={datastore} onChanged={reload} />

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
                <strong>{datastore.attachments.map((a) => a.app).join(", ")}</strong> will come up
                without a connection string the next time they are deployed.
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
  );
}

// Publishing a database on a host port.
//
// Its own section with its own warning rather than a checkbox in the
// general form: this is the difference between a database on a private
// network and a database on the internet, and the thing standing
// between the two is a firewall rule nobody here can write for you.
function ExternalAccess({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
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
              <ActionButton
                variant="outline"
                busy={busy}
                onClick={() => run(() => api.del(`${path}/expose`))}
              >
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
    </>
  );
}
