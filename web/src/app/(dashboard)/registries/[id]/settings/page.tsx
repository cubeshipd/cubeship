"use client";

import { BoxIcon, ChevronLeftIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { AWSIcon, DigitalOceanIcon } from "@/components/icons";
import { LoadingNote } from "@/components/loading";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type RegistryCredential, type RegistryStatus } from "@/lib/api";
import { message } from "@/lib/errors";

// A registry's settings: what it logs in with, and getting rid of it.
//
// The host is not here. A login is *for* a registry, so pointing an
// existing one somewhere else would silently redirect every app pulling
// through it — that is a delete and an add, and the difference is worth
// the two steps.
//
// This is also where a registry that has stopped accepting its login
// lands when you click it, because re-authenticating is the only thing
// there is to do with one.
export default function RegistrySettings({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const { org } = useOrg();

  const [credential, setCredential] = useState<RegistryCredential | null>(null);
  const [status, setStatus] = useState<RegistryStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [namespace, setNamespace] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [savingName, setSavingName] = useState(false);

  // The name goes on its own, without the login: sending it alone is
  // what corrects a typo without making someone find their token again.
  async function saveName(e: React.FormEvent) {
    e.preventDefault();
    setSavingName(true);
    setError(null);
    try {
      const updated = await api.put<RegistryCredential>(`/orgs/${org}/registries/${id}`, {
        namespace,
      });
      setCredential(updated);
      probe();
    } catch (err) {
      setError(message(err));
    }
    setSavingName(false);
  }

  useEffect(() => {
    if (!org || !id) return;
    api
      .get<RegistryCredential[]>(`/orgs/${org}/registries`)
      .then((list) => {
        const found = list.find((c) => String(c.id) === id) ?? null;
        setCredential(found);
        setNamespace(found?.namespace ?? "");
        // The username is not a secret and comes back with the listing,
        // so the field starts on it: an empty box next to a stored
        // login reads as nothing configured.
        setUsername(found?.username ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [org, id]);

  // The probe runs on arrival and again after a new login is stored,
  // because whether the new one works is the question this screen was
  // opened to answer.
  const probe = useCallback(() => {
    if (!org || !id) return;
    setStatus(null);
    api
      .get<RegistryStatus>(`/orgs/${org}/registries/${id}/status`)
      .then(setStatus)
      .catch((e) => setStatus({ state: "unreachable", detail: message(e) }));
  }, [org, id]);
  useEffect(probe, [probe]);

  const provider = credential?.provider ?? "generic";
  const aws = provider === "aws";
  const digitalocean = provider === "digitalocean";
  const Icon =
    provider === "aws" ? AWSIcon : provider === "digitalocean" ? DigitalOceanIcon : BoxIcon;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      await api.put(`/orgs/${org}/registries/${id}`, { username, password });
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
      {/* Back to the registry, not to the list: this screen is reached
          from that one, and a settings screen that returns you two
          levels up loses your place. */}
      <Link
        href={`/registries/${id}`}
        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronLeftIcon className="size-3.5" />
        {credential?.host ?? "Registry"}
      </Link>

      <PageHeader
        title={credential?.host || "Registry"}
        literal
        icon={<Icon className="size-5 shrink-0 text-muted-foreground" />}
        sub="What this organization logs in to this registry with."
        actions={<StatusBadge value={status?.state ?? "checking"} />}
      />

      <ErrorAlert error={error} />

      {!status && !error && <LoadingNote>Checking this login against the registry</LoadingNote>}

      {status?.state === "unauthorized" && (
        <Notice tone="warning">
          This registry is refusing the stored login: {status.detail}. Nothing pulls through it
          until a working one is stored, and an app whose next deploy needs it will fail.
        </Notice>
      )}
      {status?.state === "unreachable" && (
        <Notice>
          The registry did not answer: {status.detail}. That is the registry rather than the login —
          there may be nothing to fix here.
        </Notice>
      )}

      {digitalocean && (
        <>
          <SectionHeader
            title="Registry"
            sub="The one part of a registry's address that is typed rather than derived, which is the one part that can be wrong. Get it wrong and DigitalOcean answers “wrong owner”."
          />
          <Card>
            <CardContent>
              <form onSubmit={saveName} className="max-w-md space-y-4">
                <TextField
                  label="Registry name"
                  spellCheck={false}
                  value={namespace}
                  onChange={(e) => setNamespace(e.target.value)}
                  hint="What follows registry.digitalocean.com/ in an image path. Your account has one registry, and this is its name."
                />
                <div className="flex items-center gap-3">
                  <ActionButton
                    type="submit"
                    busy={savingName}
                    disabled={!namespace || namespace === (credential?.namespace ?? "")}
                  >
                    Save
                  </ActionButton>
                  <span className="text-xs text-muted-foreground">
                    Changing this changes where apps on this login pull from.
                  </span>
                </div>
              </form>
            </CardContent>
          </Card>
        </>
      )}

      <SectionHeader
        title="Login"
        sub={
          aws
            ? "The access key. What Docker logs in with is a token fetched from it at each pull, so nothing here expires."
            : "What is stored is sent to the registry as given."
        }
      />

      <Card>
        <CardContent>
          <form onSubmit={save} className="max-w-md space-y-4">
            <TextField
              label={aws ? "Access key ID" : "Username"}
              spellCheck={false}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
            {/* The password is never returned — an endpoint that
                handed it back would turn every read of this page into a
                way out for it — so the field cannot start on it. The
                placeholder says something is stored; the box being
                empty on arrival says only that you have not typed. */}
            <TextField
              label={aws ? "Secret access key" : "Password or token"}
              type="password"
              placeholder="••••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              hint="Stored and never returned, so replacing it means entering it again in full."
            />
            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={saving} disabled={!username || !password}>
                Save
              </ActionButton>
              {saved && <span className="text-xs text-muted-foreground">Login replaced.</span>}
            </div>
          </form>
        </CardContent>
      </Card>

      <DangerZone>
        <DangerAction
          title="Delete this registry"
          description={
            <>
              Apps that pulled through it keep running — a container already exists — but their next
              deploy pulls anonymously and fails if the image is private.
            </>
          }
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
        title={`Delete the login for ${credential?.host || "this registry"}?`}
        confirmWord={credential?.host}
        description="Nothing in the registry is touched. What goes is this instance's ability to pull from it."
        onConfirm={async () => {
          await api.del(`/orgs/${org}/registries/${id}`);
          router.push("/registries");
        }}
      />
    </>
  );
}
