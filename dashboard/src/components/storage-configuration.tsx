"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { CredentialDialog } from "@/components/credential-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { ResourceLimitFields } from "@/components/resource-limit-fields";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  api,
  type Credential,
  can,
  type ObjectStore,
  type ObjectStoreCredentials,
  objectStorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

const TRIGGER =
  "flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40";

export function ObjectStoreAccess({
  store,
  onChanged,
}: {
  store: ObjectStore;
  onChanged: () => void;
}) {
  return (
    <div className="space-y-6">
      {store.kind === "external" ? (
        <ExternalConnection store={store} onSaved={onChanged} />
      ) : (
        <RotateStoreCredentials store={store} onChanged={onChanged} />
      )}
      {store.kind === "managed" && <Exposure store={store} onChanged={onChanged} />}
    </div>
  );
}

function ExternalConnection({ store, onSaved }: { store: ObjectStore; onSaved: () => void }) {
  const [description, setDescription] = useState(store.description ?? "");
  const [credentialId, setCredentialId] = useState(String(store.credential_id ?? ""));
  const [bucket, setBucket] = useState(store.bucket ?? "");
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [addingAccount, setAddingAccount] = useState(false);

  const newAccountOption = "__new_account__";

  useEffect(() => {
    if (store.kind !== "external") return;
    api
      .get<Credential[]>("/credentials")
      .then(setCredentials)
      .catch(() => setCredentials([]));
  }, [store.kind]);

  async function save() {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      await api.patch(objectStorePath(store.name), {
        description,
        ...(store.kind === "external" && credentialId
          ? { credential_id: Number(credentialId) }
          : {}),
        ...(store.kind === "external" && bucket !== (store.bucket ?? "") ? { bucket } : {}),
      });
      setSaved(true);
      onSaved();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Card>
      <CardContent className="space-y-5 [&>.dashboard-section-header]:mb-0">
        <SectionHeader
          title="Connection"
          sub="Choose the account and bucket this external store uses."
        />
        <TextField
          label="Description"
          value={description}
          onChange={(e) => {
            setDescription(e.target.value);
            setSaved(false);
          }}
        />
        <div className="space-y-2">
          <GlideSelect
            label="Account"
            value={credentialId}
            onChange={(value) => {
              if (value === newAccountOption) {
                setAddingAccount(true);
                return;
              }
              setCredentialId(value);
              setSaved(false);
            }}
            options={[
              { value: newAccountOption, label: "+ New credential", searchText: "new credential" },
              ...credentials.map((credential) => ({
                value: String(credential.id),
                label: credential.label,
              })),
            ]}
            placeholder="Choose an account"
            searchable
            className="w-full"
            triggerClassName={TRIGGER}
          />
        </div>
        <TextField
          label="Bucket"
          value={bucket}
          spellCheck={false}
          onChange={(e) => {
            setBucket(e.target.value);
            setSaved(false);
          }}
          hint={
            store.bucket
              ? "This store is limited to this bucket. Clear the field to use the endpoint's default access."
              : "Set this only when the account is limited to one bucket."
          }
        />
        <ErrorAlert error={error} />
        <div className="flex items-center gap-3">
          <ActionButton busy={busy} onClick={save}>
            Save
          </ActionButton>
          {saved && <span className="text-xs text-success">Saved.</span>}
        </div>
      </CardContent>
      <CredentialDialog
        open={addingAccount}
        onOpenChange={setAddingAccount}
        onSaved={(account) => {
          setCredentials((current) =>
            [...current, account].sort((a, b) => a.label.localeCompare(b.label)),
          );
          setCredentialId(String(account.id));
          setSaved(false);
        }}
      />
    </Card>
  );
}

function RotateStoreCredentials({
  store,
  onChanged,
}: {
  store: ObjectStore;
  onChanged: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [credentials, setCredentials] = useState<ObjectStoreCredentials | null>(null);
  const path = objectStorePath(store.name);
  const me = useSession();
  const canRotate =
    can(me, "storage", "manage") &&
    (!me.grants || Boolean(me.grants.find((grant) => grant.resource === "storage")?.secrets));

  if (!canRotate) return null;

  return (
    <>
      <Card>
        <CardContent className="space-y-5">
          <SectionHeader title="Credentials" sub="Generate new S3 keys for this managed store." />
          <Notice tone="warning">
            The current keys stop working immediately. Attached apps receive the new keys on their
            next deploy.
          </Notice>
          {credentials && (
            <div className="grid gap-4 sm:grid-cols-2">
              <CopyField label="New access key id" value={credentials.access_key} />
              <CopyField label="New secret access key" value={credentials.secret_key} masked />
            </div>
          )}
          <ActionButton
            variant="outline"
            disabled={!store.has_container || store.status !== "running"}
            onClick={() => setConfirming(true)}
          >
            Rotate credentials
          </ActionButton>
        </CardContent>
      </Card>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Rotate ${store.name} credentials?`}
        confirmLabel="Rotate credentials"
        holdToConfirm
        description="The old S3 keys will stop working immediately. Update any external clients; attached apps pick up the new values on their next deploy."
        onConfirm={async () => {
          const next = await api.post<ObjectStoreCredentials>(`${path}/credentials/rotate`);
          setCredentials(next);
          onChanged();
        }}
      />
    </>
  );
}

function Exposure({ store, onChanged }: { store: ObjectStore; onChanged: () => void }) {
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [unexposing, setUnexposing] = useState(false);

  async function expose() {
    setBusy(true);
    setError(null);
    try {
      await api.post(`${objectStorePath(store.name)}/expose`, { port: Number(port) || 0 });
      setPort("");
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Card>
      <CardContent className="space-y-5 [&>.dashboard-section-header]:mb-0">
        <SectionHeader
          title="External access"
          sub="Publish a host port for connections from outside this instance."
        />
        <ErrorAlert error={error} />
        {store.exposed_port ? (
          <>
            <Notice tone="warning">
              Published on port <code>{store.exposed_port}</code> without TLS. Use a firewall rule
              to restrict access.
            </Notice>
            {store.external_endpoint && (
              <CopyField label="Endpoint" value={store.external_endpoint} />
            )}
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="outline" onClick={() => setUnexposing(true)}>
                Stop publishing
              </Button>
              <Link href="/firewall" className="text-sm text-muted-foreground underline">
                Firewall rules
              </Link>
            </div>
          </>
        ) : (
          <form
            className="max-w-xl space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void expose();
            }}
          >
            <div className="max-w-xl space-y-2">
              <Label htmlFor="object-store-host-port">Host port</Label>
              <div className="flex gap-2">
                <Input
                  id="object-store-host-port"
                  value={port}
                  inputMode="numeric"
                  spellCheck={false}
                  placeholder="Choose automatically"
                  onChange={(event) => setPort(event.target.value.replace(/\D/g, ""))}
                  className="h-10 min-w-0 flex-1"
                />
                <ActionButton type="submit" variant="outline" busy={busy} className="h-10 shrink-0">
                  Publish port
                </ActionButton>
              </div>
              <p className="text-xs text-muted-foreground">
                Automatic ports use 16000–16999. Publishing replaces the container briefly; its data
                is preserved.
              </p>
            </div>
          </form>
        )}
        <ConfirmDialog
          open={unexposing}
          onOpenChange={setUnexposing}
          title="Stop publishing this store?"
          confirmLabel="Stop publishing"
          description="It returns to private access for apps on this instance. External clients lose their connection while the container is replaced."
          onConfirm={async () => {
            await api.del(`${objectStorePath(store.name)}/expose`);
            setUnexposing(false);
            onChanged();
          }}
        />
      </CardContent>
    </Card>
  );
}

export function ObjectStoreResources({
  store,
  onSaved,
}: {
  store: ObjectStore;
  onSaved: () => void;
}) {
  const limits = store.limits ?? { cpu: 0, memory_bytes: 0 };
  const [cpu, setCPU] = useState(limits.cpu ? String(limits.cpu) : "");
  const [memory, setMemory] = useState(
    limits.memory_bytes ? String(Math.round(limits.memory_bytes / (1 << 20))) : "",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const next = { cpu: Number(cpu) || 0, memory_bytes: (Number(memory) || 0) * (1 << 20) };
  const dirty = next.cpu !== limits.cpu || next.memory_bytes !== limits.memory_bytes;

  return (
    <Card>
      <CardContent className="space-y-5 [&>.dashboard-section-header]:mb-0">
        <SectionHeader
          title="Resource limits"
          sub="Set optional CPU and memory ceilings for the managed store."
        />
        <form
          className="space-y-4"
          onSubmit={async (event) => {
            event.preventDefault();
            setBusy(true);
            setError(null);
            try {
              await api.patch<ObjectStore>(objectStorePath(store.name), { limits: next });
              onSaved();
            } catch (err) {
              setError(message(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <ResourceLimitFields
            cpu={cpu}
            memory={memory}
            onCPUChange={setCPU}
            onMemoryChange={setMemory}
          />
          <ErrorAlert error={error} />
          <ActionButton type="submit" busy={busy} disabled={!dirty}>
            Save
          </ActionButton>
        </form>
      </CardContent>
    </Card>
  );
}
