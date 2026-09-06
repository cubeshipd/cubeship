"use client";

import { ChevronLeftIcon, EyeIcon, PlugIcon, SettingsIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { CopyButton } from "@/components/copy-button";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ValueCard } from "@/components/value-card";
import {
  type App,
  api,
  type Datastore,
  type DatastoreCredentials,
  datastoreLabel,
  datastorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One database. Its name is the whole of the address, because it
// belongs to the instance rather than to a project.
export default function DatastorePage({ params }: PageProps<"/databases/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

// The two things there are to know about a database, as tabs: where it
// answers, and who is wired to it. Settings is its own page, like every
// other resource here — the actions that cannot be undone belong at the
// bottom of a page you went to on purpose.
type Tab = "overview" | "apps";

function Detail({ name }: { name: string }) {
  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [tab, setTab] = useState<Tab>("overview");
  const [error, setError] = useState<string | null>(null);

  const path = datastorePath(name);
  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then(setDatastore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // Provisioning is a container being pulled and started, detached from
  // the request that asked for it — so this is the one screen that has
  // to go back and look. It stops the moment it settles.
  useEffect(() => {
    if (datastore?.status !== "provisioning") return;
    const timer = setInterval(reload, 2000);
    return () => clearInterval(timer);
  }, [datastore?.status, reload]);

  if (error && !datastore) return <ErrorAlert error={error} />;
  if (!datastore) return null;

  return (
    <>
      <Link
        href="/databases"
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        databases
      </Link>

      <PageHeader
        title={
          <span className="font-mono text-lg tracking-normal normal-case">{datastore.name}</span>
        }
        sub={
          <span className="flex items-center gap-2">
            {datastoreLabel(datastore.engine)} {datastore.version}
            <StatusBadge value={datastore.status} />
          </span>
        }
        actions={
          <Button
            variant="outline"
            nativeButton={false}
            render={
              <Link href={`/databases/${datastore.name}/settings`}>
                <SettingsIcon />
                Settings
              </Link>
            }
          />
        }
      />

      <ErrorAlert error={error} />

      {/* The reason a database did not come up is the tail of what the
          engine printed, and it appears nowhere else — the container it
          came from has been removed. It is above the tabs because it is
          about the whole thing, not one view of it. */}
      {datastore.status === "failed" && datastore.error && (
        <Card className="mb-4 border-l-2 border-destructive/30 border-l-destructive">
          <CardContent>
            <div className="text-[11px] tracking-[0.12em] text-destructive uppercase">
              It did not start
            </div>
            <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted-foreground">
              {datastore.error}
            </pre>
          </CardContent>
        </Card>
      )}

      <div className="mb-5">
        <Tabs value={tab} onValueChange={(v) => setTab(String(v) as Tab)}>
          <TabsList>
            <TabsTrigger value="overview" className="px-3 text-xs">
              Overview
            </TabsTrigger>
            <TabsTrigger value="apps" className="px-3 text-xs">
              Apps
              {datastore.attachments.length > 0 && (
                <span className="ml-1.5 font-mono text-subtle-foreground">
                  {datastore.attachments.length}
                </span>
              )}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {tab === "overview" ? (
        <Connection datastore={datastore} />
      ) : (
        <Attachments datastore={datastore} onChanged={reload} />
      )}
    </>
  );
}

// Where it answers, and what to connect with.
//
// The address an app uses is on the page; the password is a click away.
// Both are true of the same thing, and the difference is that one of
// them is a credential — a screen you leave open at a desk should not
// have it on it by default.
function Connection({ datastore }: { datastore: Datastore }) {
  const [creds, setCreds] = useState<DatastoreCredentials | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const internal = creds ? creds.internal_uri : `${datastore.host}:${datastore.port}`;

  async function reveal() {
    setBusy(true);
    setError(null);
    try {
      setCreds(await api.get<DatastoreCredentials>(`${datastorePath(datastore.name)}/credentials`));
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <ErrorAlert error={error} />

      {!creds && (
        <div className="mb-4 flex justify-end">
          <ActionButton variant="outline" size="sm" busy={busy} onClick={reveal}>
            <EyeIcon />
            Show credentials
          </ActionButton>
        </div>
      )}

      <ValueCard
        label="From an app on this instance"
        value={
          <span className="flex items-start justify-between gap-2">
            <span>{internal}</span>
            <CopyButton value={internal} />
          </span>
        }
      />

      {datastore.exposed_port ? (
        <ValueCard
          label="From anywhere else"
          value={
            <span className="flex items-start justify-between gap-2">
              <span>
                {creds?.external_uri ??
                  `${datastore.external_host ?? "this host"}:${datastore.exposed_port}`}
              </span>
              <CopyButton
                value={
                  creds?.external_uri ??
                  `${datastore.external_host ?? ""}:${datastore.exposed_port}`
                }
              />
            </span>
          }
        />
      ) : null}

      <Card className="mb-4">
        <CardContent>
          <Table>
            <TableBody>
              <TableRow>
                <TableCell className="w-40 text-muted-foreground">Username</TableCell>
                <TableCell className="font-mono">{datastore.username}</TableCell>
              </TableRow>
              {datastore.database && (
                <TableRow>
                  <TableCell className="text-muted-foreground">Database</TableCell>
                  <TableCell className="font-mono">{datastore.database}</TableCell>
                </TableRow>
              )}
              <TableRow>
                <TableCell className="text-muted-foreground">Password</TableCell>
                <TableCell className="font-mono">
                  {creds ? (
                    <span className="flex items-center gap-1">
                      {creds.password}
                      <CopyButton value={creds.password} />
                    </span>
                  ) : (
                    <span className="text-subtle-foreground">hidden</span>
                  )}
                </TableCell>
              </TableRow>
              {datastore.description && (
                <TableRow>
                  <TableCell className="text-muted-foreground">Description</TableCell>
                  <TableCell>{datastore.description}</TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </>
  );
}

// Which apps get this database's connection variables.
//
// The variable names are listed rather than summarised, because the
// question somebody has on this screen is "what do I read in my code",
// and the answer is exactly this list.
function Attachments({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [attaching, setAttaching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function detach(appRef: string) {
    setError(null);
    try {
      await api.del(`${datastorePath(datastore.name)}/attachments/${appRef}`);
      onChanged();
    } catch (err) {
      setError(message(err));
    }
  }

  return (
    <>
      <ErrorAlert error={error} />

      <div className="mb-4 flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          Each app here receives the connection string as environment variables, from its next
          deploy onwards. They may be in any project.
        </p>
        <Button variant="outline" size="sm" onClick={() => setAttaching(true)}>
          <PlugIcon />
          Attach an app
        </Button>
      </div>

      {datastore.attachments.length === 0 ? (
        <Notice>
          Nothing is attached, so no app on this instance can reach this database yet. Attaching one
          gives it <code className="text-foreground">DATABASE_URL</code> and its parts.
        </Notice>
      ) : (
        <Card className="mb-4">
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>App</TableHead>
                  <TableHead>Variables it receives</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {datastore.attachments.map((a) => (
                  <TableRow key={a.app}>
                    <TableCell className="font-mono">
                      <Link href={`/projects/${a.app}`} className="hover:text-primary">
                        {a.app}
                      </Link>
                    </TableCell>
                    {/* The variable names wrap rather than scroll. The
                        question this column answers is "what do I read
                        in my code", and an answer you have to drag
                        sideways to finish reading is a worse answer
                        than a taller row. */}
                    <TableCell className="whitespace-normal">
                      <span className="flex flex-wrap gap-1">
                        {a.variables.map((v) => (
                          <code
                            key={v}
                            className="border border-border bg-secondary/50 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                          >
                            {v}
                          </code>
                        ))}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Detach ${a.app}`}
                        onClick={() => detach(a.app)}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Notice>
        A container keeps the environment it was created with, so attaching or detaching changes
        nothing until the app is deployed again.
      </Notice>

      <AttachDialog
        datastore={datastore}
        open={attaching}
        onOpenChange={setAttaching}
        onAttached={onChanged}
      />
    </>
  );
}

function AttachDialog({
  datastore,
  open,
  onOpenChange,
  onAttached,
}: {
  datastore: Datastore;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAttached: () => void;
}) {
  const [apps, setApps] = useState<App[] | null>(null);
  const [app, setApp] = useState("");
  const [prefix, setPrefix] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setApp("");
    setPrefix("");
    setError(null);
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch((e) => setError(message(e)));
  }, [open]);

  // Every app on the instance, minus the ones already attached — the
  // daemon refuses those, and offering a choice it would refuse is a
  // form that argues with you.
  const attached = new Set(datastore.attachments.map((a) => a.app));
  const candidates = (apps ?? []).filter((a) => !attached.has(a.reference));

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`${datastorePath(datastore.name)}/attachments`, { app, prefix });
      onAttached();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Attach an app</DialogTitle>
            <DialogDescription>
              It will receive <code className="text-foreground">{prefix}DATABASE_URL</code> and its
              parts on its next deploy.
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[55vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} className="mb-0" />

            {apps && candidates.length === 0 ? (
              <Notice>
                Every app on this instance is already attached, or there are none yet.
              </Notice>
            ) : (
              <div className="flex flex-wrap gap-2">
                {candidates.map((a) => (
                  <Button
                    key={a.reference}
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-pressed={a.reference === app}
                    onClick={() => setApp(a.reference)}
                    className={
                      a.reference === app
                        ? "neon-edge border-primary/60 bg-primary/8 font-mono text-foreground"
                        : "font-mono"
                    }
                  >
                    {a.reference}
                  </Button>
                ))}
              </div>
            )}

            <TextField
              label="Prefix"
              value={prefix}
              spellCheck={false}
              placeholder="ANALYTICS_"
              onChange={(e) => setPrefix(e.target.value.toUpperCase())}
              hint="Leave empty unless this app already has a database. Two under the same prefix would be one variable with two values."
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!app}>
              Attach
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
