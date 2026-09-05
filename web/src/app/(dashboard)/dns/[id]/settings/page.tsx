"use client";

import { ChevronLeftIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { CloudflareIcon } from "@/components/icons";
import { LoadingNote } from "@/components/loading";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type DNSCredential, type DNSStatus } from "@/lib/api";
import { DNS_PROVIDERS } from "@/lib/dns";
import { message } from "@/lib/errors";

// A DNS provider's settings: what it is called, what it authenticates
// with, and getting rid of it.
//
// The provider itself is not here. A credential is *for* one provider —
// how it authenticates and what its secret even is both follow from
// that — so changing it in place would not be an edit, it would be a
// different credential wearing the old one's id.
export default function DNSSettings({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();

  const [credential, setCredential] = useState<DNSCredential | null>(null);
  const [status, setStatus] = useState<DNSStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [label, setLabel] = useState("");
  const [savingLabel, setSavingLabel] = useState(false);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (id) return;
    api
      .get<DNSCredential[]>(`/dns`)
      .then((list) => {
        const found = list.find((c) => String(c.id) === id) ?? null;
        setCredential(found);
        setLabel(found?.label ?? "");
        // The key id is not a secret and comes back with the listing, so
        // the field starts on it: an empty box beside a stored
        // credential reads as nothing configured.
        setUsername(found?.username ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [id]);

  // The probe runs on arrival and again after a new credential is
  // stored, because whether the new one works is the question this
  // screen was opened to answer.
  const probe = useCallback(() => {
    if (id) return;
    setStatus(null);
    api
      .get<DNSStatus>(`/dns/${id}/status`)
      .then(setStatus)
      .catch((e) => setStatus({ state: "unreachable", detail: message(e) }));
  }, [id]);
  useEffect(probe, [probe]);

  const shape = credential ? DNS_PROVIDERS[credential.provider] : null;
  const Icon = shape?.icon ?? CloudflareIcon;

  async function saveLabel(e: React.FormEvent) {
    e.preventDefault();
    setSavingLabel(true);
    setError(null);
    try {
      setCredential(await api.patch<DNSCredential>(`/dns/${id}`, { label }));
    } catch (err) {
      setError(message(err));
    }
    setSavingLabel(false);
  }

  async function saveCredential(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      // The key id travels with the secret where the provider has one:
      // a new secret against the old id is not a credential anybody
      // chose, and it fails in a way that reads as the secret being
      // wrong.
      await api.patch(`/dns/${id}`, {
        password,
        ...(shape?.userLabel ? { username } : {}),
      });
      setPassword("");
      setSaved(true);
      probe();
    } catch (err) {
      setError(message(err));
    }
    setSaving(false);
  }

  return (
    <>
      {/* Back to the provider, not to the list: this screen is reached
          from that one, and returning two levels up loses your place. */}
      <Link
        href={`/dns/${id}`}
        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronLeftIcon className="size-3.5" />
        {credential?.label || "DNS"}
      </Link>

      <PageHeader
        title={credential?.label || "DNS provider"}
        icon={<Icon className="size-5 shrink-0 text-muted-foreground" />}
        sub={
          shape
            ? `What this organization reaches ${shape.label} with.`
            : "What this organization reaches its DNS provider with."
        }
        actions={<StatusBadge value={status?.state ?? "checking"} />}
      />

      <ErrorAlert error={error} />

      {!status && !error && (
        <LoadingNote>Checking this credential against the provider</LoadingNote>
      )}

      {status?.state === "unauthorized" && (
        <Notice tone="warning">
          The provider is refusing the stored credential: {status.detail}. No record can be read or
          written through it until a working one is stored.
        </Notice>
      )}
      {status?.state === "unreachable" && (
        <Notice>
          The provider did not answer: {status.detail}. That is their API rather than the credential
          — there may be nothing to fix here.
        </Notice>
      )}

      <SectionHeader
        title="Label"
        sub="What tells this account from another on the same provider. It is a label, not an identifier — changing it moves nothing."
      />
      <Card>
        <CardContent>
          <form onSubmit={saveLabel} className="max-w-md space-y-4">
            <TextField label="Label" value={label} onChange={(e) => setLabel(e.target.value)} />
            <ActionButton
              type="submit"
              busy={savingLabel}
              disabled={!label || label === credential?.label}
            >
              Save
            </ActionButton>
          </form>
        </CardContent>
      </Card>

      <SectionHeader
        title="Credential"
        sub="Stored as given — the provider takes the secret itself, so a hash could not be sent to one — and never returned."
      />
      <Card>
        <CardContent>
          <form onSubmit={saveCredential} className="max-w-md space-y-4">
            {shape?.userLabel && (
              <TextField
                label={shape.userLabel}
                spellCheck={false}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            )}
            <TextField
              label={shape?.secretLabel ?? "Secret"}
              type="password"
              placeholder="••••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              hint={shape?.hint}
            />
            <div className="flex items-center gap-3">
              <ActionButton
                type="submit"
                busy={saving}
                disabled={!password || (shape?.userLabel !== "" && !username)}
              >
                Save
              </ActionButton>
              {saved && <span className="text-xs text-muted-foreground">Credential replaced.</span>}
            </div>
          </form>
        </CardContent>
      </Card>

      <DangerZone>
        <DangerAction
          title="Delete this provider"
          description="Nothing at the provider is touched and no record changes. What goes is this instance's ability to read or write them."
          action={
            <Button variant="destructive" onClick={() => setDeleting(true)}>
              <Trash2Icon />
              Delete
            </Button>
          }
        />
      </DangerZone>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${credential?.label ?? "this provider"}?`}
        confirmWord={credential?.label}
        description="Your records stay exactly as they are. Cubeship just stops being able to reach them."
        onConfirm={async () => {
          await api.del(`/dns/${id}`);
          router.push("/dns");
        }}
      />
    </>
  );
}
