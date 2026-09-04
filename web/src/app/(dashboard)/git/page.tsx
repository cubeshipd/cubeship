"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLinkIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubAppCard } from "@/components/github-app-card";
import { GitHubIcon } from "@/components/icons";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { useSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type GitHubConnections, type GitHubInstallation, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

// Where an organization's code comes from.
//
// Two layers, and the page says which is which because they belong to
// different people. **The App** is the instance's, registered once by
// whoever runs the VPS — one App, one set of credentials, and only a
// super-admin can touch it. **The connections** are this organization's:
// each is that App installed on a GitHub account, and it is what makes
// this organization able to clone that account's private code.
//
// Nothing here is per app or per project. An installation covers an
// account, and every app built from a repository on that account uses
// it.
export default function GitProviders() {
  const { org, loaded } = useOrg();
  const me = useSession();
  const queries = useQueryClient();

  const [disconnecting, setDisconnecting] = useState<GitHubInstallation | null>(null);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });

  const connections = useQuery({
    queryKey: ["github", org],
    queryFn: () => api.get<GitHubConnections>(`/orgs/${org}/github`),
    enabled: Boolean(org),
  });

  const slug = settings.data?.github_app_slug ?? "";
  const registered = Boolean(settings.data?.github_connected);

  // GitHub's own page for an installation, which is where repository
  // access is granted or widened. `installations/new` is the App's
  // entry point: for an account that already has it, GitHub shows the
  // configure page rather than starting again.
  //
  // It is not the App's *permissions* page — that one belongs to
  // whoever owns the App, is instance-wide, and every installation has
  // to accept a change to it. The two are linked separately below for
  // that reason.
  const configureURL = slug ? `https://github.com/apps/${slug}/installations/new` : "";

  const columns: Column<GitHubInstallation>[] = [
    {
      id: "account",
      header: "Account",
      width: 40,
      sortBy: (i) => i.account,
      cell: (i) => (
        <span className="inline-flex items-center gap-2 font-mono text-xs">
          <GitHubIcon className="size-3.5 shrink-0 text-muted-foreground" />
          {i.account}
        </span>
      ),
    },
    {
      id: "installation",
      header: "Installation",
      width: 25,
      sortBy: (i) => i.installation_id,
      cell: (i) => (
        <span className="font-mono text-xs text-muted-foreground">{i.installation_id}</span>
      ),
    },
    {
      id: "connected",
      header: "Connected",
      width: 20,
      sortBy: (i) => i.created_at,
      cell: (i) => (
        <span className="text-xs text-muted-foreground">
          {new Date(i.created_at).toLocaleDateString()}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 15,
      align: "right",
      cell: (i) => (
        <div className="flex items-center justify-end gap-2">
          {configureURL && (
            <Button
              variant="ghost"
              size="xs"
              nativeButton={false}
              aria-label={`Configure ${i.account} on GitHub`}
              className="size-6 p-0 text-muted-foreground"
              render={
                <a href={configureURL} target="_blank" rel="noreferrer noopener">
                  <ExternalLinkIcon className="size-3.5" />
                </a>
              }
            />
          )}
          <Button
            variant="ghost"
            size="xs"
            aria-label={`Disconnect ${i.account}`}
            className="size-6 p-0 text-muted-foreground hover:text-destructive"
            onClick={() => setDisconnecting(i)}
          >
            <Trash2Icon className="size-3.5" />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Git Providers"
        sub="Where this organization's code is read from. A connection is one GitHub account, and it is what lets Cubeship clone that account's private repositories and deploy on a push to them."
        actions={
          registered &&
          connections.data?.install_url && (
            <Button
              nativeButton={false}
              render={
                <a href={connections.data.install_url} target="_blank" rel="noreferrer noopener">
                  <PlusIcon />
                  Connect an account
                </a>
              }
            />
          )
        }
      />

      <ErrorAlert error={connections.error ? message(connections.error) : null} />

      {loaded && !org && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No organization selected. A connection belongs to one — pick or create an organization
            from the switcher at the top of the sidebar.
          </CardContent>
        </Card>
      )}

      {org && !registered && settings.isSuccess && (
        <Notice>
          This instance is not registered as a GitHub App yet, so there is nothing to install.
          {me.is_super_admin
            ? " Register it below."
            : " Ask whoever runs this instance to register it."}
        </Notice>
      )}

      {org && registered && (
        <>
          <SectionHeader
            title="Connected accounts"
            sub="Each is the instance's GitHub App installed on one account. Configure opens GitHub, where you choose which repositories it can see — that is where you widen access."
          />
          <DataTable
            columns={columns}
            rows={connections.data?.installations ?? (connections.isLoading ? null : [])}
            rowKey={(i) => String(i.id)}
            loadingRows={2}
            empty="No accounts connected yet. Connect one and its repositories become available to every app in this organization."
          />
        </>
      )}

      {/* The App itself: the instance's, not this organization's. Only a
          super-admin can register or replace it, and doing so is a
          different act from connecting an account — which is why it sits
          under its own heading rather than beside the table. */}
      {me.is_super_admin && settings.data && (
        <>
          <SectionHeader
            title="The GitHub App"
            sub="One per instance, registered by whoever runs it. Every organization here installs this same App on its own accounts."
          />
          <GitHubAppCard
            settings={settings.data}
            onSaved={(s) => queries.setQueryData(["settings"], s)}
          />
          {slug && (
            <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
              To change what the App may do at all — its permissions — go to{" "}
              <a
                href={`https://github.com/settings/apps/${slug}/permissions`}
                target="_blank"
                rel="noreferrer noopener"
                className="text-foreground underline underline-offset-4"
              >
                its settings on GitHub
              </a>
              . That is instance-wide, and every account it is installed on has to accept the change
              before it takes effect there.
            </p>
          )}
        </>
      )}

      <ConfirmDialog
        open={disconnecting !== null}
        onOpenChange={(v) => !v && setDisconnecting(null)}
        title={`Disconnect ${disconnecting?.account}?`}
        confirmWord={disconnecting?.account}
        description={
          <>
            The App stays installed on GitHub — this only stops Cubeship using it. Apps built from
            that account&apos;s private repositories stop being able to clone, and a push to one
            stops deploying anything. Containers already running are untouched.{" "}
            <Link href="/" className="underline underline-offset-4">
              Their apps
            </Link>{" "}
            keep serving.
          </>
        }
        confirmLabel="Disconnect"
        onConfirm={async () => {
          if (!disconnecting) return;
          await api.del(`/orgs/${org}/github/${disconnecting.id}`);
          await connections.refetch();
          setDisconnecting(null);
        }}
      />
    </>
  );
}
