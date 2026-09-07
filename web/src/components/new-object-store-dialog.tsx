"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { OptionCards } from "@/components/option-cards";
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
// **OptionCards, not a select**, and this is the case it exists for:
// the two are not two spellings of one thing. One starts a server on
// this box and puts the bytes on its disk — which is where deleting it
// takes them with it — and the other writes down an address and a key
// for somebody else's. Picking wrong is expensive and the difference is
// a sentence, which is the whole test.
//
// The provider *is* a select, for the mirror reason: which S3 this is
// is a choice between named things, and a grid of cards for it would be
// a paragraph per option nobody reads twice.
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

  const asks = catalogue?.providers.find((p) => p.provider === provider)?.asks;

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

            <OptionCards
              value={kind}
              onChange={setKind}
              options={[
                {
                  value: "external",
                  title: "Link a bucket",
                  body: "An endpoint somewhere else — S3, R2, Spaces, anything that speaks S3. This instance stores the keys and nothing else; removing it later leaves the bucket untouched.",
                },
                {
                  value: "managed",
                  title: "Run MinIO here",
                  body: "A server on this machine, on the same disk as everything else. Good for an app's uploads and for developing against S3 — and not for a backup of this machine, which needs to be somewhere else.",
                },
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
              hint="What this storage is for. Optional, and the only place that can say — nothing sits above a store."
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
                    hint={
                      provider === "digitalocean"
                        ? "The Space's region, e.g. nyc3 — the endpoint is derived from it."
                        : "e.g. eu-central-1. The endpoint is derived from it."
                    }
                  />
                )}
                {asks === "account" && (
                  <TextField
                    label="Account id"
                    value={account}
                    spellCheck={false}
                    onChange={(e) => setAccount(e.target.value)}
                    hint="R2's endpoint is named after it: the hex string in the S3 API address on your R2 page."
                  />
                )}
                {asks === "endpoint" && (
                  <TextField
                    label="Endpoint"
                    value={endpoint}
                    spellCheck={false}
                    onChange={(e) => setEndpoint(e.target.value)}
                    hint="Paste what the provider shows you, URL and all: https://s3.eu-central-1.wasabisys.com."
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
                      hint="Stored as given — a signature cannot be computed from a hash — and never shown again. It appears under Credentials, ready to be picked next time."
                    />
                  </>
                )}

                <TextField
                  label="Bucket"
                  value={bucket}
                  spellCheck={false}
                  onChange={(e) => setBucket(e.target.value)}
                  hint="Leave empty unless this key reaches exactly one bucket and may not list them — an R2 token scoped to a bucket, a narrow IAM policy. Naming one here pins the store to it."
                />
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
