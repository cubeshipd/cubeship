"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";

// production is the environment every project is created with, and the
// one it can never lose: every app assumes it exists, so it goes when
// the project does and never before.
const PRODUCTION = "production";

export default function EnvironmentSettingsPage({
  params,
}: PageProps<"/projects/[project]/[env]/settings">) {
  return <Settings {...use(params)} />;
}

// An environment's settings, which is one irreversible act — and for
// `production`, not even that.
//
// It was the project's three sections and is now the project's one, for
// the same reason: the General section held a description and a slug
// shown read-only, and neither is a thing any more.
function Settings({ project, env }: { project: string; env: string }) {
  const router = useRouter();
  const [deleting, setDeleting] = useState(false);

  if (!project || !env) {
    return (
      <p className="text-sm text-muted-foreground">
        No environment named.{" "}
        <Link href="/projects" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }

  const isProduction = env === PRODUCTION;

  return (
    <>
      <Link
        href={`/projects/${project}/${env}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {project}/{env}
      </Link>

      <PageHeader title="Environment settings" />

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
          await api.del(`/projects/${project}/environments/${env}`);
          router.push(`/projects/${project}`);
        }}
      />
    </>
  );
}
