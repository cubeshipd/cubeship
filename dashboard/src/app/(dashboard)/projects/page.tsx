"use client";

import { useQuery } from "@tanstack/react-query";
import { BoxesIcon, PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { PageLoading } from "@/components/page-loading";
import { useSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { can } from "@/lib/api";
import { projectsQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

export default function ProjectsPage() {
  const router = useRouter();
  const me = useSession();
  const projects = useQuery(projectsQuery);
  const [creatingApp, setCreatingApp] = useState(false);
  const canCreateProject =
    me.grants === null ||
    me.grants.some(
      (grant) => grant.resource === "projects" && grant.level === "manage" && grant.items === null,
    );
  const canCreateApp = can(me, "apps", "manage");
  const canCreateWorkspace = canCreateProject && canCreateApp;

  useEffect(() => {
    const first = projects.data?.[0];
    if (first && !projects.isFetching && !projects.isError) {
      router.replace(`/projects/${first.slug}/production`);
    }
  }, [projects.data, projects.isError, projects.isFetching, router]);

  if (projects.isError) {
    return (
      <div className="max-w-xl space-y-4">
        <ErrorAlert error={message(projects.error)} />
        <Button variant="outline" onClick={() => void projects.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  if (projects.isPending || projects.isFetching || projects.data?.length) {
    return <PageLoading kind="grid" label="Opening projects" />;
  }

  return (
    <>
      <Card className="w-full flex-1 text-center">
        <CardContent className="flex h-full min-h-[24rem] flex-col items-center justify-center px-6 py-12">
          <span className="mb-5 flex size-14 items-center justify-center rounded-2xl border border-primary/20 bg-primary/5 text-primary">
            <BoxesIcon className="size-7" />
          </span>
          <h1 className="text-xl font-medium tracking-tight">Your workspace is ready</h1>
          <p className="mt-2 max-w-sm text-sm leading-6 text-muted-foreground">
            {canCreateWorkspace
              ? "Create your first app and project together. You can configure its source and deploy it after creation."
              : "There are no projects you can use yet. Ask an administrator to create or share one with you."}
          </p>
          <Button
            className="mt-6"
            disabled={!canCreateWorkspace}
            onClick={() => setCreatingApp(true)}
          >
            <PlusIcon />
            Create your first app
          </Button>
        </CardContent>
      </Card>
      <NewWorkloadAppDialog
        open={creatingApp}
        onOpenChange={setCreatingApp}
        onCreated={(reference) => {
          setCreatingApp(false);
          router.replace(`/projects/${reference}`);
        }}
      />
    </>
  );
}
