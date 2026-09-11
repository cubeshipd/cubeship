"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { api, type Credential, type ObjectStore, objectStorePath } from "@/lib/api";
import { message } from "@/lib/errors";

// A store's settings: what can be changed about it, and the two acts
// that cannot be undone.
//
// Its own page rather than a header full of buttons, like every other
// resource here: renaming a thing, describing it and destroying it are
// not the same kind of act, and the last one belongs at the bottom of a
// page you went to on purpose.
export default function ObjectStoreSettingsPage({ params }: PageProps<"/storage/[name]/settings">) {
  const { name } = use(params);
  return <Settings name={name} />;
}

function Settings({ name }: { name: string }) {
  const router = useRouter();
  const [store, setStore] = useState<ObjectStore | null>(null);
  const [error, setError] = useState<string | null>(null);

  const path = objectStorePath(name);
  const reload = useCallback(() => {
    api
      .get<ObjectStore>(path)
      .then(setStore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  return (
    <>
      {" "}
      <ErrorAlert error={error} />
      {!store && !error && <LoadingList rows={3} />}
      {store && (
        <>
          <General store={store} onSaved={reload} />
          {store.kind === "managed" && <StoreLimits store={store} onSaved={reload} />}
          {store.kind === "managed" && <Exposure store={store} onChanged={reload} />}

          <DangerZone className="mt-10">
            <DangerAction
              title="Delete this store"
              description={
                store.kind === "managed" ? (
                  <>
                    Its container is removed and{" "}
                    <strong>every object on this host goes with it</strong>. There is no backup and
                    nothing to restore it from.
                  </>
                ) : (
                  <>
                    This instance forgets the address and the account it uses.{" "}
                    <strong>Nothing in the bucket is touched</strong> — it stays exactly where it
                    is, and linking it again brings it back.
                  </>
                )
              }
              action={<DeleteButton store={store} onDeleted={() => router.push("/storage")} />}
            />
          </DangerZone>
        </>
      )}
    </>
  );
}

// StoreLimits caps what a managed store's container may take.
//
// Offered only for a managed store, and refused by the daemon for the
// other kind: a linked store runs on somebody else's server, so there is
// no container here to cap and a number on this screen would be one that
// does nothing.
function StoreLimits({ store, onSaved }: { store: ObjectStore; onSaved: () => void }) {
  const [cpu, setCPU] = useState(store.limits.cpu ? String(store.limits.cpu) : "");
  const [memory, setMemory] = useState(
    store.limits.memory_bytes ? String(Math.round(store.limits.memory_bytes / (1 << 20))) : "",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const next = { cpu: Number(cpu) || 0, memory_bytes: (Number(memory) || 0) * (1 << 20) };
  const dirty = next.cpu !== store.limits.cpu || next.memory_bytes !== store.limits.memory_bytes;

  return (
    <>
      <SectionHeader
        title="Limits"
        sub="How much of the machine this store's container may take. Empty is no limit, which is the default. Changing one takes effect within seconds and does not replace the container the way publishing a port does; removing one waits for the next start."
      />
      <ErrorAlert error={error} />
      <div className="space-y-4">
        <TextField
          label="CPU"
          type="number"
          step="0.01"
          min="0"
          placeholder="no limit"
          value={cpu}
          onChange={(e) => setCPU(e.target.value)}
          hint="Cores, fractional allowed: 0.5 is half a core. A ceiling rather than a share — at its limit the container is throttled, not merely preferred less."
        />
        <TextField
          label="Memory (MiB)"
          type="number"
          min="0"
          placeholder="no limit"
          value={memory}
          onChange={(e) => setMemory(e.target.value)}
          hint="A hard ceiling. The kernel enforces it by killing whatever crosses it, so setting one below what the store is already using stops it there and then — which an upload in flight sees as the connection going away."
        />
        <Button
          disabled={busy || !dirty}
          onClick={async () => {
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
          {busy ? "Saving..." : "Save limits"}
        </Button>
      </div>
    </>
  );
}

// What is editable, which is nearly nothing — and the name says why.
function General({ store, onSaved }: { store: ObjectStore; onSaved: () => void }) {
  const [description, setDescription] = useState(store.description ?? "");
  const [credentialId, setCredentialId] = useState(String(store.credential_id ?? ""));
  const [bucket, setBucket] = useState(store.bucket ?? "");
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

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
        // Sent only when it moved, because empty is a value here — it
        // is how a store is unpinned — and sending it unasked would
        // unpin one on every save of the description.
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
    <section className="mb-8">
      <SectionHeader title="General" />
      <ErrorAlert error={error} />

      <div className="space-y-4">
        {/* Read-only, and the hint is the reason rather than a
            decoration: the name is a managed store's container, which
            apps resolve, and the endpoint is what anything configured
            against this store already points at. */}
        <CopyField
          label="Name"
          value={store.name}
          hint="Permanent. It is the container's name for a store this instance runs, and the identity for either kind."
        />
        <CopyField
          label="Endpoint"
          value={store.endpoint}
          hint="Permanent. Re-pointing it in place would silently send every app configured against this store somewhere else."
        />

        <TextField
          label="Description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          hint="What this storage is for. With nothing above a store, this is the only place that can say."
        />

        {/* Which account it authenticates as is the one part of the
            connection that is meant to change: a second key, a rotated
            token, a different tenancy. A managed store has no account
            to re-point — its keys are its own. */}
        {store.kind === "external" && (
          <SearchableSelect
            label="Account"
            searchable={credentials.length > 8}
            value={credentialId}
            onChange={setCredentialId}
            choices={credentials.map((c) => ({ value: String(c.id), label: c.label }))}
            hint="Rotating the secret itself is one edit under Credentials, and every store on that account follows it."
          />
        )}

        {/* Every linked store, pinned or not. Clearing the field is the
            direction that can never be wrong, and setting one is
            accepted wherever linking accepts it — a managed store is
            the only refusal, and it has no section here. */}
        {store.kind === "external" && (
          <TextField
            label="Bucket"
            value={bucket}
            spellCheck={false}
            onChange={(e) => setBucket(e.target.value)}
            hint={
              store.bucket
                ? "This store is pinned to this one bucket: it is the only one listed, and creating or reaching another is refused. Clear the field to hand that question back to the endpoint."
                : "Only for a key issued for one bucket. Naming one here is the store giving up every other bucket in it, including making new ones."
            }
          />
        )}

        <div className="flex items-center gap-3">
          <ActionButton busy={busy} onClick={save}>
            Save
          </ActionButton>
          {saved && <span className="text-xs text-success">Saved.</span>}
        </div>
      </div>
    </section>
  );
}

// Publishing a managed store on a host port.
//
// Off by default and worth leaving off, which is why it is here rather
// than on the create form: it is a deliberate act with a consequence
// somebody should be reading about at the moment they take it.
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
    <section className="mb-8">
      <SectionHeader
        title="Reaching it from outside"
        sub="Apps on this instance already reach it by name. This is for everything else — an S3 client on your laptop, a backup script on another machine."
      />
      <ErrorAlert error={error} />

      {store.exposed_port ? (
        <>
          <Notice tone="warning">
            Published on port {store.exposed_port}, with <strong>no TLS in front of it</strong>: the
            signature protects the keys and nothing protects what is being transferred. What makes
            that safe is a firewall rule, which is yours to write — there is a{" "}
            <Link href="/firewall" className="underline">
              screen for it
            </Link>
            .
          </Notice>
          <div className="mt-4 space-y-4">
            {store.external_endpoint && (
              <CopyField label="Endpoint" value={store.external_endpoint} />
            )}
            <Button variant="outline" onClick={() => setUnexposing(true)}>
              Stop publishing it
            </Button>
          </div>
          <ConfirmDialog
            open={unexposing}
            onOpenChange={setUnexposing}
            title="Stop publishing this store?"
            confirmLabel="Stop publishing"
            description="It goes back to being reachable only by apps on this instance. Anything off this host that is pointed at it stops working — and the container is replaced to drop the port, so it is briefly down for the apps too."
            onConfirm={async () => {
              await api.del(`${objectStorePath(store.name)}/expose`);
              setUnexposing(false);
              onChanged();
            }}
          />
        </>
      ) : (
        <div className="space-y-4">
          <p className="max-w-2xl text-sm leading-relaxed text-muted-foreground">
            Publishing it puts a plain HTTP endpoint on this machine's network interfaces. The
            container is replaced to pick the port up — published ports are fixed when a container
            is created — and the objects survive that untouched.
          </p>
          <TextField
            label="Port"
            value={port}
            spellCheck={false}
            onChange={(e) => setPort(e.target.value)}
            hint="Leave it empty to take one from 16000–16999. Name one if you already have a firewall rule written."
          />
          <ActionButton busy={busy} onClick={expose}>
            Publish it
          </ActionButton>
        </div>
      )}
    </section>
  );
}

function DeleteButton({ store, onDeleted }: { store: ObjectStore; onDeleted: () => void }) {
  const [confirming, setConfirming] = useState(false);
  return (
    <>
      <Button variant="destructive" onClick={() => setConfirming(true)}>
        Delete
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Delete this store?"
        confirmLabel="Delete"
        confirmWord={store.name}
        description={
          store.kind === "managed" ? (
            <>
              The container goes and <strong>every object in it goes with it</strong>. Nothing here
              keeps a copy.
            </>
          ) : (
            <>
              This instance forgets where it is and which account reaches it. The bucket and
              everything in it are untouched.
            </>
          )
        }
        onConfirm={async () => {
          await api.del(objectStorePath(store.name));
          onDeleted();
        }}
      />
    </>
  );
}
