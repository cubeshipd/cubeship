"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type Credential } from "@/lib/api";
import { message } from "@/lib/errors";

// Adding a credential, and editing one.
//
// It asks for a label, a first half where the secret has one, and the
// secret. Nothing else: a credential carries no provider, because what
// it is *for* is decided where it is wired up — a registry, a DNS
// provider — and one may be doing both at once. Most API tokens can
// only be read at the moment they are issued, so a secret filed under
// one job is a secret you have to issue twice.
export function CredentialDialog({
  credential,
  title,
  open,
  onOpenChange,
  onSaved,
}: {
  credential?: Credential;
  // What the dialog calls itself, for a screen where "credential" is
  // not the word somebody has in mind.
  title?: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  // The saved credential, for a caller that wants to use it straight
  // away rather than only reload a list.
  onSaved: (saved: Credential) => void;
}) {
  const editing = credential !== undefined;
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    setPassword("");
    setLabel(credential?.label ?? "");
    setUsername(credential?.username ?? "");
  }, [open, credential]);

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
            username,
            ...(password ? { password } : {}),
          })
        : await api.post<Credential>("/credentials", { label, username, password });
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
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            <TextField
              label="Label"
              value={label}
              spellCheck={false}
              autoFocus
              className="scroll-mt-12"
              onChange={(e) => setLabel(e.target.value)}
            />

            {/* Optional, because a bare token is a normal credential:
                whether a login has two halves is decided by whatever
                the secret is wired to, not here. */}
            <TextField
              label="Username or key ID"
              value={username}
              spellCheck={false}
              placeholder="Leave empty for a bare token"
              onChange={(e) => setUsername(e.target.value)}
            />

            <TextField
              label="Secret"
              type="password"
              value={password}
              spellCheck={false}
              onChange={(e) => setPassword(e.target.value)}
              hint={editing ? "Leave empty to keep the secret it has." : undefined}
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!label || (!editing && !password)}>
              {editing ? "Save" : "Add credential"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
