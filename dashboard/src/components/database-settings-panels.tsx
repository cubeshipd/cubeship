"use client";

import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { Backups } from "@/components/backups";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { ResourceLimitFields } from "@/components/resource-limit-fields";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, can, type Datastore, type DatastoreCredentials, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

export function DatabaseBackups({ datastore }: { datastore: Datastore }) {
  return (
    <Backups
      database={datastore.name}
      canBackUp={datastore.can_back_up ?? false}
      consistency={datastore.backup_consistency}
      hasContainer={datastore.has_container}
    />
  );
}

export function DatabaseLimits({
  datastore,
  onSaved,
}: {
  datastore: Datastore;
  onSaved: (datastore: Datastore) => void;
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
    <Card>
      <CardContent className="space-y-5">
        <SectionHeader
          title="Resource limits"
          sub="Set optional CPU and memory ceilings for this database."
        />
        <form
          className="space-y-5"
          onSubmit={async (event) => {
            event.preventDefault();
            setBusy(true);
            setError(null);
            try {
              onSaved(await api.patch<Datastore>(datastorePath(datastore.name), { limits: next }));
            } catch (cause) {
              setError(message(cause));
            } finally {
              setBusy(false);
            }
          }}
        >
          <ErrorAlert error={error} />
          <ResourceLimitFields
            cpu={cpu}
            memory={memory}
            onCPUChange={setCPU}
            onMemoryChange={setMemory}
          />
          <ActionButton type="submit" busy={busy} disabled={!dirty}>
            Save
          </ActionButton>
        </form>
      </CardContent>
    </Card>
  );
}

export function DatabaseAccess({
  datastore,
  onChanged,
}: {
  datastore: Datastore;
  onChanged: () => void;
}) {
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [unpublishing, setUnpublishing] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [rotatedCredentials, setRotatedCredentials] = useState<DatastoreCredentials | null>(null);
  const [error, setError] = useState<string | null>(null);
  const path = datastorePath(datastore.name);
  const me = useSession();
  const canRotate =
    can(me, "databases", "manage") &&
    (!me.grants || Boolean(me.grants.find((grant) => grant.resource === "databases")?.secrets));

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await action();
      setPort("");
      onChanged();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Card>
        <CardContent className="space-y-5">
          <header className="space-y-1">
            <h2 className="text-lg font-medium tracking-tight">External access</h2>
            <p className="text-sm text-muted-foreground">
              Publish a host port for connections from outside this instance.
            </p>
          </header>
          <ErrorAlert error={error} />
          {datastore.exposed_port ? (
            <div className="space-y-4">
              <Notice tone="warning">
                Published on{" "}
                <code className="font-mono">
                  {datastore.external_host ?? "this host"}:{datastore.exposed_port}
                </code>
                . Database traffic has no TLS; access is protected by its password and your
                firewall.
              </Notice>
              <ActionButton variant="outline" busy={busy} onClick={() => setUnpublishing(true)}>
                Stop publishing
              </ActionButton>
            </div>
          ) : (
            <div className="max-w-xl space-y-2">
              <Label htmlFor="db-host-port">Host port</Label>
              <div className="flex gap-2">
                <Input
                  id="db-host-port"
                  value={port}
                  inputMode="numeric"
                  spellCheck={false}
                  placeholder="Choose automatically"
                  onChange={(event) => setPort(event.target.value.replace(/\D/g, ""))}
                  className="h-10 min-w-0 flex-1"
                />
                <ActionButton
                  variant="outline"
                  busy={busy}
                  className="h-10 shrink-0"
                  onClick={() =>
                    run(() => api.post(`${path}/expose`, { port: port ? Number(port) : 0 }))
                  }
                >
                  Publish port
                </ActionButton>
              </div>
              <p className="text-xs text-muted-foreground">Automatic ports use 15000–15999.</p>
            </div>
          )}
        </CardContent>
      </Card>

      {canRotate && (
        <Card>
          <CardContent className="space-y-5">
            <header className="space-y-1">
              <h2 className="text-lg font-medium tracking-tight">Database credentials</h2>
              <p className="text-sm text-muted-foreground">
                Generate a new password for this database login.
              </p>
            </header>
            <Notice tone="warning">
              The current password stops working immediately. Attached apps receive the new password
              on their next deploy.
            </Notice>
            {rotatedCredentials && (
              <div className="grid gap-4 sm:grid-cols-2">
                <CopyField label="New password" value={rotatedCredentials.password} masked />
                <CopyField
                  label="New internal connection string"
                  value={rotatedCredentials.internal_uri}
                  masked
                  fieldClassName="sm:col-span-2"
                />
              </div>
            )}
            <ActionButton
              variant="outline"
              disabled={!datastore.has_container || datastore.status !== "running"}
              onClick={() => setRotating(true)}
            >
              Rotate credentials
            </ActionButton>
          </CardContent>
        </Card>
      )}

      <ConfirmDialog
        open={unpublishing}
        onOpenChange={setUnpublishing}
        title={`Stop publishing ${datastore.name}?`}
        confirmLabel="Stop publishing"
        description="Connections using the host port are interrupted while the container is replaced. Apps on this instance keep their private network connection."
        onConfirm={async () => {
          await run(() => api.del(`${path}/expose`));
          setUnpublishing(false);
        }}
      />
      <ConfirmDialog
        open={rotating}
        onOpenChange={setRotating}
        title={`Rotate ${datastore.name} credentials?`}
        confirmLabel="Rotate credentials"
        holdToConfirm
        description="The old password will stop working immediately. Update any external clients; attached apps pick up the new value on their next deploy."
        onConfirm={async () => {
          const credentials = await api.post<DatastoreCredentials>(`${path}/credentials/rotate`);
          setRotatedCredentials(credentials);
          onChanged();
        }}
      />
    </>
  );
}
