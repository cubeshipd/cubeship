"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { Credential, DNSProvider, DNSProviderKind } from "@/lib/api";
import { api } from "@/lib/api";
import { providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// The value that means "type a login here instead of picking one".
//
// It is an option in the credential select rather than a mode switch
// beside it, because it is an answer to the same question: which secret
// does this write through. A first provider therefore takes no trip to
// the Credentials screen — a credential is a convenience, not a
// prerequisite.
const NEW_CREDENTIAL = "";

export function DNSProviderDialog({
  kinds,
  credentials,
  provider,
  open,
  onOpenChange,
  onSaved,
}: {
  kinds: DNSProviderKind[];
  credentials: Credential[];
  // The row being re-pointed, or undefined when connecting a new one.
  provider?: DNSProvider;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const editing = provider !== undefined;
  const [kind, setKind] = useState("");
  const [credentialID, setCredentialID] = useState(NEW_CREDENTIAL);
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    setLabel("");
    setUsername("");
    setPassword("");
    setKind(provider?.provider ?? kinds[0]?.provider ?? "");
    // With nothing stored there is nothing to pick, so the form opens
    // on the half that always works.
    setCredentialID(
      provider ? String(provider.credential_id) : (credentials[0]?.id.toString() ?? NEW_CREDENTIAL),
    );
  }, [open, provider, kinds, credentials]);

  const spec = kinds.find((k) => k.provider === kind);
  const typing = credentialID === NEW_CREDENTIAL;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (editing) {
        await api.patch(`/dns/${provider.id}`, { credential_id: Number(credentialID) });
      } else {
        await api.post(
          "/dns",
          typing
            ? { provider: kind, label, username, password }
            : { provider: kind, credential_id: Number(credentialID) },
        );
      }
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const complete = editing
    ? credentialID !== NEW_CREDENTIAL
    : Boolean(kind) && (typing ? Boolean(label && password) : true);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>
              {editing ? `Edit ${provider.provider_name}` : "New DNS provider"}
            </DialogTitle>
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            {/* Which API is spoken is what the provider *is*, so it is
                fixed once the row exists — changing it in place would
                be a different provider wearing the same id. */}
            <SearchableSelect
              label="Provider"
              searchable={false}
              disabled={editing}
              value={kind}
              onChange={setKind}
              choices={kinds.map((k) => ({
                value: k.provider,
                label: k.name,
                icon: providerIcon(k.provider),
              }))}
            />

            <SearchableSelect
              label="Credential"
              searchable={false}
              value={credentialID}
              onChange={setCredentialID}
              choices={[
                ...credentials.map((c) => ({ value: String(c.id), label: c.label })),
                ...(editing ? [] : [{ value: NEW_CREDENTIAL, label: "Type a new one…" }]),
              ]}
            />

            {typing && !editing && (
              <>
                <TextField
                  label="Label"
                  value={label}
                  spellCheck={false}
                  onChange={(e) => setLabel(e.target.value)}
                />
                {/* Absent where the secret is a single value: a token
                    has no name beside it, and a field asking for one
                    would be a field with no right answer. */}
                {spec?.username_label && (
                  <TextField
                    label={spec.username_label}
                    value={username}
                    spellCheck={false}
                    onChange={(e) => setUsername(e.target.value)}
                  />
                )}
                <TextField
                  label={spec?.password_label ?? "Secret"}
                  type="password"
                  value={password}
                  spellCheck={false}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!complete}>
              {editing ? "Save" : "Connect"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
