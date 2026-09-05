"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextAreaField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { api, type Environment } from "@/lib/api";
import { message } from "@/lib/errors";

// production is the environment every project is created with, and the
// daemon refuses to delete it — so an app can always assume its project
// has somewhere to live.
const PRODUCTION = "production";

export default function EnvironmentSettingsPage({
  params,
}: {
  params: Promise<{ org: string; project: string; env: string }>;
}) {
  return <Settings {...use(params)} />;
}

function Settings({ org, project, env }: { org: string; project: string; env: string }) {
  const router = useRouter();
  const ref = `${org}/${project}/${env}`;

  const [current, setCurrent] = useState<Environment | null>(null);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const projectPath = `/orgs/${org}/projects/${project}`;
  const path = `${projectPath}/environments/${env}`;

  // There is no "get one environment" endpoint — the list is the read,
  // and a project holds a handful of them.
  useEffect(() => {
    if (!org || !project || !env) return;
    api
      .get<Environment[]>(`${projectPath}/environments`)
      .then((list) => {
        const found = list.find((e) => e.slug === env);
        if (!found) throw new Error("environment not found");
        setCurrent(found);
        setDescription(found.description ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [projectPath, org, project, env]);

  if (!ref || !env) {
    return (
      <p className="text-sm text-muted-foreground">
        No environment named.{" "}
        <Link href="/" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }

  const isProduction = env === PRODUCTION;
  const dirty = !!current && description !== (current.description ?? "");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      setCurrent(await api.patch<Environment>(path, { description }));
      setSaved(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <Link
        href={`/projects/${org}/${project}/${env}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {org}/{project}/{env}
      </Link>

      <PageHeader
        title="Environment settings"
        sub="What this stage of the project is called, and what it is for."
      />

      <ErrorAlert error={error} />

      <SectionHeader title="General" />
      <Card>
        <CardContent>
          <form onSubmit={save} className="space-y-4">
            <TextAreaField
              label="Description"
              hint="What runs here, and who it is for. Empty is fine."
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={!current}
              placeholder="Where a change goes before production."
            />

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Slug</Label>
              <div className="flex h-10 items-center border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                {org}/{project}/{env}
              </div>
              <p className="text-xs text-subtle-foreground">
                Not editable. It is the third component of every app reference in this environment.
              </p>
            </div>

            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty}>
                Save
              </ActionButton>
              {saved && !dirty && <span className="text-xs text-muted-foreground">Saved.</span>}
            </div>
          </form>
        </CardContent>
      </Card>

      <DangerZone>
        <DangerAction
          title="Delete this environment"
          description={
            isProduction ? (
              <>
                <code>production</code> is created with the project and cannot be deleted — an app
                and a deploy both assume every project has at least one environment.
              </>
            ) : (
              <>
                Removes the environment and every app deployed in it. Each app&apos;s container is
                stopped and removed first.
              </>
            )
          }
          action={
            <Button variant="destructive" disabled={isProduction} onClick={() => setDeleting(true)}>
              Delete environment
            </Button>
          }
        />
      </DangerZone>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete environment"
        description="The environment, the variables set on it and every app in it go — containers included. This cannot be undone."
        confirmWord={env}
        confirmLabel="Delete environment"
        onConfirm={async () => {
          await api.del(path);
          router.push(`/projects/${org}/${project}`);
        }}
      />
    </>
  );
}
