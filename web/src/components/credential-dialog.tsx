"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { OptionCards } from "@/components/option-cards";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  type Credential,
  type CredentialCapability,
  type CredentialProvider,
} from "@/lib/api";
import { CAPABILITY_LABELS } from "@/lib/credentials";
import { message } from "@/lib/errors";

// Adding an account, and editing one.
//
// It lives here rather than on the credentials page because the pages
// that *use* credentials open it too: a credential is a convenience,
// not a prerequisite, so "add one" has to be reachable from the screen
// you are already on. `capability` is what narrows the providers to the
// ones that can do that screen's job — the DNS page offers no
// registry-only account, because storing one there would be storing a
// secret for a job it cannot do.
//
// One dialog for both acts, because they ask for the same things minus
// one: the provider is permanent — what a credential is for is what its
// secret is — so editing offers the label and the secret and nothing
// else.
export function CredentialDialog({
  providers,
  credential,
  capability,
  title,
  description,
  open,
  onOpenChange,
  onSaved,
}: {
  providers: CredentialProvider[];
  credential?: Credential;
  capability?: CredentialCapability;
  // What the dialog calls itself, for a screen where "credential" is
  // not the word somebody has in mind — the DNS page is adding a DNS
  // provider, and it happens to be an account.
  title?: string;
  description?: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  // The saved credential, for a caller that wants to use it straight
  // away rather than only reload a list.
  onSaved: (saved: Credential) => void;
}) {
  const offered = capability
    ? providers.filter((p) => p.capabilities.includes(capability))
    : providers;
  const editing = credential !== undefined;
  const [provider, setProvider] = useState("");
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    setPassword("");
    setProvider(credential?.provider ?? offered[0]?.provider ?? "");
    setLabel(credential?.label ?? "");
    setUsername(credential?.username ?? "");
  }, [open, credential, offered]);

  const chosen = providers.find((p) => p.provider === provider);
  const needsUsername = !!chosen?.username_label;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const saved = editing
        ? // An empty secret means "leave it alone", which is what makes
          // renaming one not a rotation.
          await api.patch<Credential>(`/credentials/${credential.id}`, {
            label,
            ...(needsUsername ? { username } : {}),
            ...(password ? { password } : {}),
          })
        : await api.post<Credential>("/credentials", {
            provider,
            label,
            ...(needsUsername ? { username } : {}),
            password,
          });
      onSaved(saved);
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>
              {editing ? `Edit ${credential.label}` : (title ?? "New credential")}
            </DialogTitle>
            <DialogDescription>
              {editing
                ? "Rotating the secret here rotates it everywhere: everything authenticating with this account follows."
                : (description ??
                  "It becomes available to everything its provider can be used for.")}
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            {editing ? (
              <Notice>
                The provider stays <strong>{credential.provider_name}</strong>. What a credential is
                for is what its secret is — moving one to another provider means adding a credential
                and deleting this.
              </Notice>
            ) : (
              <OptionCards
                label="Provider"
                value={provider}
                onChange={setProvider}
                options={offered.map((p) => ({
                  value: p.provider,
                  title: p.name,
                  body: `${p.capabilities.map((c) => CAPABILITY_LABELS[c] ?? c).join(" and ")} — ${p.hint}`,
                }))}
              />
            )}

            <TextField
              label="Label"
              value={label}
              spellCheck={false}
              autoFocus={editing}
              onChange={(e) => setLabel(e.target.value)}
              hint="How you will recognise it. “the AWS one” stops identifying anything the moment there are two."
            />

            {needsUsername && (
              <TextField
                label={chosen.username_label ?? ""}
                value={username}
                spellCheck={false}
                onChange={(e) => setUsername(e.target.value)}
              />
            )}

            <TextField
              label={chosen?.password_label ?? "Secret"}
              type="password"
              value={password}
              spellCheck={false}
              onChange={(e) => setPassword(e.target.value)}
              hint={
                editing
                  ? "Leave empty to keep the secret it has. Anything here replaces it."
                  : "Stored as given, because the provider takes the secret itself. No endpoint returns it."
              }
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton
              type="submit"
              busy={busy}
              disabled={!label || !provider || (!editing && !password)}
            >
              {editing ? "Save" : "Add credential"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
