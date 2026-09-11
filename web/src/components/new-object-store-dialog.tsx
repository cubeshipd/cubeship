"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { SearchableSelect } from "@/components/searchable-select";
import { SlugField } from "@/components/slug-field";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  type Credential,
  type ObjectStore,
  type ObjectStoreKind,
  type ObjectStoreProviders,
} from "@/lib/api";
import { message } from "@/lib/errors";

// Adding object storage, both ways.
//
// A select, like every other choice between named things here. The two
// options are named well enough to choose between — one is somebody
// else's server, the other is this machine — and what each of them
// costs belongs on the page where it is acted on, not in a paragraph
// nobody reads twice in front of a form.
export function NewObjectStoreDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (created: ObjectStore) => void;
}) {
  const [kind, setKind] = useState<ObjectStoreKind>("external");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const [catalogue, setCatalogue] = useState<ObjectStoreProviders | null>(null);
  const [provider, setProvider] = useState("aws");
  const [region, setRegion] = useState("");
  const [account, setAccount] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [bucket, setBucket] = useState("");

  // A stored account, or keys typed here. Empty means "type them",
  // because a credential is a convenience and not a prerequisite: the
  // first store somebody links is linked before there is an account to
  // pick.
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credentialId, setCredentialId] = useState("");
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setKind("external");
    setName("");
    setDescription("");
    setRegion("");
    setAccount("");
    setEndpoint("");
    setBucket("");
    setCredentialId("");
    setAccessKey("");
    setSecretKey("");
    setError(null);

    api
      .get<ObjectStoreProviders>("/objectstores/providers")
      .then((found) => {
        setCatalogue(found);
        if (found.providers[0]) setProvider(found.providers[0].provider);
      })
      .catch((e) => setError(message(e)));
    // Credentials are an admin's to read, and everything in this dialog
    // already is. An instance with none simply offers the typed pair.
    api
      .get<Credential[]>("/credentials")
      .then(setCredentials)
      .catch(() => setCredentials([]));
  }, [open]);

  const chosen = catalogue?.providers.find((p) => p.provider === provider);
  const asks = chosen?.asks;
  const scoped = chosen?.scopes_by_bucket ?? false;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<ObjectStore>(
        "/objectstores",
        kind === "managed"
          ? { kind, name, description }
          : {
              kind,
              name,
              description,
              provider,
              region,
              account,
              endpoint,
              bucket,
              ...(credentialId
                ? { credential_id: Number(credentialId) }
                : { new_access_key: accessKey, new_secret_key: secretKey }),
            },
      );
      onCreated(created);
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const ready =
    name !== "" &&
    (kind === "managed" ||
      ((credentialId !== "" || (accessKey !== "" && secretKey !== "")) &&
        (asks !== "region" || region !== "") &&
        (asks !== "account" || account !== "") &&
        (asks !== "endpoint" || endpoint !== "")));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Add object storage</DialogTitle>
          </DialogHeader>

          {/* The same scroll container every long form here uses: it
              clips both axes, so the padding gives the 1px borders and
              focus rings room and the negative margin puts the content
              back where it was. */}
          <div className="-mx-2 max-h-[65vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            <SearchableSelect
              label="Kind"
              searchable={false}
              value={kind}
              onChange={(v) => setKind(v as ObjectStoreKind)}
              choices={[
                { value: "external", label: "Link a bucket somewhere else" },
                { value: "managed", label: "Run MinIO on this machine" },
              ]}
            />

            <SlugField
              label="Name"
              value={name}
              onChange={setName}
              autoFocus
              className="scroll-mt-12"
            />

            <TextField
              label="Description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />

            {kind === "external" && (
              <>
                <SearchableSelect
                  label="Provider"
                  searchable={false}
                  value={provider}
                  onChange={setProvider}
                  choices={(catalogue?.providers ?? []).map((p) => ({
                    value: p.provider,
                    label: p.label,
                  }))}
                />

                {/* One field, whichever it is: each provider's endpoint
                    is a template with a single variable in it, and the
                    daemon says which. */}
                {asks === "region" && (
                  <TextField
                    label="Region"
                    value={region}
                    spellCheck={false}
                    onChange={(e) => setRegion(e.target.value)}
                    hint={provider === "digitalocean" ? "e.g. nyc3" : "e.g. eu-central-1"}
                  />
                )}
                {asks === "account" && (
                  <TextField
                    label="Account id"
                    value={account}
                    spellCheck={false}
                    onChange={(e) => setAccount(e.target.value)}
                    hint="The hex string in the S3 API address on your R2 page."
                  />
                )}
                {asks === "endpoint" && (
                  <TextField
                    label="Endpoint"
                    value={endpoint}
                    spellCheck={false}
                    onChange={(e) => setEndpoint(e.target.value)}
                    hint="Paste the URL your provider shows you."
                  />
                )}

                <SearchableSelect
                  label="Account"
                  searchable={credentials.length > 8}
                  value={credentialId}
                  onChange={setCredentialId}
                  choices={[
                    { value: "", label: "Type the keys here" },
                    ...credentials.map((c) => ({ value: String(c.id), label: c.label })),
                  ]}
                />

                {/* Typed keys become a stored account in the same
                    request, so the second store is one choice rather
                    than a second copy of the same secret. */}
                {credentialId === "" && (
                  <>
                    <TextField
                      label="Access key id"
                      value={accessKey}
                      spellCheck={false}
                      onChange={(e) => setAccessKey(e.target.value)}
                    />
                    <TextField
                      label="Secret access key"
                      type="password"
                      value={secretKey}
                      spellCheck={false}
                      onChange={(e) => setSecretKey(e.target.value)}
                      hint="Never shown again. It appears under Credentials, ready to be picked next time."
                    />
                  </>
                )}

                {/* Only where the provider's own logins are issued per
                    bucket. Offered everywhere it was a field somebody
                    filled in because it was there, and naming one gives
                    up listing, creating and deleting for the whole
                    store. */}
                {scoped && (
                  <TextField
                    label="Bucket"
                    value={bucket}
                    spellCheck={false}
                    onChange={(e) => setBucket(e.target.value)}
                    hint="Optional, and only for a key issued for one bucket. Leave it empty for a key that reaches the account — naming one here is the store giving up every other bucket, including making new ones."
                  />
                )}
              </>
            )}

            {kind === "managed" && (
              <p className="text-xs leading-relaxed text-subtle-foreground">
                Its keys are generated and shown on the store's own page. It comes up reachable only
                by apps on this instance; publishing it on a host port is a separate, deliberate act
                in its settings.
              </p>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!ready}>
              {kind === "managed" ? "Create" : "Link"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
