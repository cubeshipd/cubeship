"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubAppCard } from "@/components/github-app-card";
import { ConnectGitHub } from "@/components/github-connect";
import { GitHubIcon } from "@/components/icons";
import { useOrg } from "@/components/org-context";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type GitHubConnections, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

// One row per provider, and today there is one provider.
//
// A connection is a whole relationship with a provider, not one account
// inside it: connecting GitHub once and then adding a second
// organization to it is *configuring* that connection, not making
// another. So the accounts it covers are a column, and both actions —
// widen it, or end it — act on the connection.
type Provider = {
  id: string;
  name: string;
  icon: typeof GitHubIcon;
  connected: boolean;
  // The GitHub accounts this connection reaches: a personal account, an
  // organization, several organizations.
  accounts: string[];
  // Where GitHub lets someone add an account or widen the repositories
  // an existing one exposes.
  configureURL: string;
};

export default function GitProviders() {
  const { org, loaded } = useOrg();
  const me = useSession();
  const [disconnecting, setDisconnecting] = useState(false);

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
  const installations = connections.data?.installations ?? [];

  const providers: Provider[] | null =
    settings.isLoading || (Boolean(org) && connections.isLoading)
      ? null
      : [
          {
            id: "github",
            name: "GitHub",
            icon: GitHubIcon,
            connected: installations.length > 0,
            accounts: installations.map((i) => i.account),
            // `installations/new` is the App's own entry point: for an
            // account that already has it, GitHub shows the configure
            // page instead of starting again — which is the same door
            // for "add an organization" and "let it see one more repo".
            configureURL: slug ? `https://github.com/apps/${slug}/installations/new` : "",
          },
        ];

  const github = providers?.[0];
  const columns: Column<Provider>[] = [
    {
      id: "provider",
      header: "Provider",
      width: 24,
      cell: (p) => (
        <span className="inline-flex items-center gap-2 text-sm">
          <p.icon className="size-4 shrink-0" />
          {p.name}
        </span>
      ),
    },
    {
      id: "accounts",
      header: "Accounts",
      width: 46,
      cell: (p) =>
        p.accounts.length > 0 ? (
          <span className="font-mono text-xs" title={p.accounts.join("\n")}>
            {p.accounts.join(", ")}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        ),
    },
    {
      id: "status",
      header: "Status",
      width: 16,
      cell: (p) => <StatusBadge value={p.connected ? "available" : "stopped"} />,
    },
    {
      id: "actions",
      header: "",
      width: 14,
      align: "right",
      cell: (p) =>
        p.connected && (
          <div className="flex items-center justify-end gap-2">
            {p.configureURL && (
              <Button
                variant="ghost"
                size="xs"
                nativeButton={false}
                aria-label="Configure on GitHub"
                className="size-6 p-0 text-muted-foreground"
                render={
                  <a href={p.configureURL} target="_blank" rel="noreferrer noopener">
                    <ExternalLinkIcon className="size-3.5" />
                  </a>
                }
              />
            )}
            <Button
              variant="ghost"
              size="xs"
              aria-label="Disconnect"
              className="size-6 p-0 text-muted-foreground hover:text-destructive"
              onClick={() => setDisconnecting(true)}
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
        sub="Where this organization's code is read from. A connection lets Cubeship clone private repositories and deploy on a push to them."
        actions={
          org &&
          github &&
          (github.connected ? (
            <Button
              variant="outline"
              nativeButton={false}
              render={
                <a href={github.configureURL} target="_blank" rel="noreferrer noopener">
                  <ExternalLinkIcon />
                  Configure on GitHub
                </a>
              }
            />
          ) : (
            <ConnectGitHub settings={settings.data} instanceName={settings.data?.domain ?? ""} />
          ))
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

      {org && (
        <>
          <SectionHeader
            title="Connected accounts"
            sub="Configure opens GitHub, where you add an organization or let it see more repositories. Both go through the same page there — adding a second organization is widening this connection, not making another."
          />
          <DataTable columns={columns} rows={providers} rowKey={(p) => p.id} loadingRows={1} />
        </>
      )}

      {/* The escape hatch, and deliberately a link rather than a
          section: an App made by hand is a real thing to have and not
          something anyone should read about on the way to connecting. */}
      {org && me.is_super_admin && !settings.data?.github_connected && settings.data && (
        <div className="mt-4">
          <GitHubAppCard settings={settings.data} onSaved={() => settings.refetch()} />
        </div>
      )}

      <ConfirmDialog
        open={disconnecting}
        onOpenChange={setDisconnecting}
        title={
          installations.length > 1
            ? `Disconnect GitHub, and all ${installations.length} accounts with it?`
            : "Disconnect GitHub?"
        }
        confirmWord="disconnect"
        description={
          <>
            The App stays installed on GitHub — this only stops Cubeship using it. Apps built from
            private repositories on{" "}
            <code>{installations.map((i) => i.account).join(", ") || "those accounts"}</code> stop
            being able to clone, and a push to one stops deploying anything. Containers already
            running keep serving.
          </>
        }
        confirmLabel="Disconnect"
        onConfirm={async () => {
          // Every installation, because the connection is the thing
          // being ended — leaving one behind would be a connection the
          // page says is gone.
          for (const i of installations) {
            await api.del(`/orgs/${org}/github/${i.id}`);
          }
          await connections.refetch();
          setDisconnecting(false);
        }}
      />
    </>
  );
}
