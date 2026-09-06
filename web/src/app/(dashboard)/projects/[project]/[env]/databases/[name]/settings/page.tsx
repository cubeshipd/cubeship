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
import { TextAreaField, TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { api, type Datastore, datastoreLabel, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

export default function DatastoreSettingsPage({
  params,
}: PageProps<"/projects/[project]/[env]/databases/[name]/settings">) {
  const { project, env, name } = use(params);
  return <Settings project={project} env={env} name={name} />;
}

function Settings({ project, env, name }: { project: string; env: string; name: string }) {
  const router = useRouter();
  const reference = `${project}/${env}/${name}`;
  const path = datastorePath(reference);

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
      const updated = await api.patch<Datastore>(path, { description });
      setDatastore(updated);
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
        href={`/projects/${project}/${env}/databases/${name}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {reference}
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
              hint="What this database holds. Empty is fine."
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
                    <TableCell className="text-xs text-subtle-foreground">
                      It is part of the address every attached app connects to.
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell className="text-muted-foreground">Engine</TableCell>
                    <TableCell className="font-mono">
                      {datastoreLabel(datastore.engine)} {datastore.version}
                    </TableCell>
                    <TableCell className="text-xs text-subtle-foreground">
                      A data directory written by one major version cannot be read by another.
                      Changing version means a second database and a migration.
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell className="text-muted-foreground">Username</TableCell>
                    <TableCell className="font-mono">{datastore.username}</TableCell>
                    <TableCell className="text-xs text-subtle-foreground">
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
                <strong>{datastore.attachments.map((a) => a.app).join(", ")}</strong> will fail to
                connect on their next deploy.
              </>
            ) : (
              "no app is attached to it."
            )}
          </>
        }
        confirmWord={datastore.name}
        onConfirm={async () => {
          await api.del(path);
          router.push(`/projects/${project}/${env}`);
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

  async function expose() {
    setBusy(true);
    setError(null);
    try {
      await api.post(`${datastorePath(datastore.reference)}/expose`, {
        port: port ? Number(port) : 0,
      });
      setPort("");
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  async function unexpose() {
    setBusy(true);
    setError(null);
    try {
      await api.del(`${datastorePath(datastore.reference)}/expose`);
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

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
              <ActionButton variant="outline" busy={busy} onClick={unexpose}>
                Stop publishing it
              </ActionButton>
            </>
          ) : (
            <>
              <Notice>
                Reachable only from apps on this instance, which is the right answer for almost
                every database.
              </Notice>
              <div className="flex items-end gap-3">
                <TextField
                  label="Host port"
                  value={port}
                  spellCheck={false}
                  placeholder="auto"
                  onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
                  fieldClassName="w-40"
                  hint="Leave empty to take the next free one from 15000-15999."
                />
                <ActionButton variant="outline" busy={busy} onClick={expose}>
                  Publish it
                </ActionButton>
              </div>
              <p className="text-xs leading-relaxed text-subtle-foreground">
                The container is replaced to pick the port up, so the database restarts. Its data is
                a directory on the host and is not touched. You still have to open the port in your
                firewall.
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </>
  );
}
