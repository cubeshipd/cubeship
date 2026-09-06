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
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
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
  const me = useSession();
  const [disconnecting, setDisconnecting] = useState(false);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });

  const connections = useQuery({
    queryKey: ["github"],
    queryFn: () => api.get<GitHubConnections>(`/github`),
  });

  const slug = settings.data?.github_app_slug ?? "";
  const installations = connections.data?.installations ?? [];

  const providers: Provider[] | null =
    settings.isLoading || connections.isLoading
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

  const _github = providers?.[0];
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
      width: 40,
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
      width: 20,
      align: "right",
      // Connecting acts on this provider, so it belongs on this
      // provider's row — not in the page header, which is where the
      // page's own actions go and where it read as "connect something".
      cell: (p) =>
        p.connected ? (
          <RowActions>
            {p.configureURL && (
              <RowAction
                icon={ExternalLinkIcon}
                label="Configure on GitHub"
                title="Add an organization, or let it see more repositories"
                href={p.configureURL}
              />
            )}
            <RowAction
              icon={Trash2Icon}
              label="Disconnect"
              danger
              onClick={() => setDisconnecting(true)}
            />
          </RowActions>
        ) : (
          <div className="flex justify-end">
            <ConnectGitHub
              settings={settings.data}
              instanceName={settings.data?.domain ?? ""}
              size="sm"
            />
          </div>
        ),
    },
  ];

  return (
    <>
      <PageHeader title="Git Providers" />

      <ErrorAlert error={connections.error ? message(connections.error) : null} />

      {/* An App registered before Cubeship asked for OAuth on install
          was registered private too, and a private GitHub App installs
          only on the account that owns it — which is why the install
          page offers no organizations. Neither can be changed after the
          fact, so the only thing that helps is a new App. */}
      {settings.data?.github_connected && settings.data?.github_oauth_ready === false && (
        <Notice tone="warning">
          This instance&apos;s GitHub App was registered before Cubeship could install on
          organizations, so GitHub only offers your personal account. It cannot be changed — an App
          is public or private from the moment it is created — so connecting again below registers a
          new one.
          {settings.data.github_app_slug && (
            <>
              {" "}
              Delete the old one from{" "}
              <a
                href={`https://github.com/settings/apps/${settings.data.github_app_slug}/advanced`}
                target="_blank"
                rel="noreferrer noopener"
                className="text-foreground underline underline-offset-4"
              >
                its settings on GitHub
              </a>{" "}
              first, or the new one will need a different name — GitHub App names are unique across
              all of GitHub.
            </>
          )}
        </Notice>
      )}

      <DataTable columns={columns} rows={providers} rowKey={(p) => p.id} loadingRows={1} />

      {/* The escape hatch, and deliberately a link rather than a
          section: an App made by hand is a real thing to have and not
          something anyone should read about on the way to connecting. */}
      {me.is_super_admin && !settings.data?.github_connected && settings.data && (
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
            await api.del(`/github/${i.id}`);
          }
          await connections.refetch();
          setDisconnecting(false);
        }}
      />
    </>
  );
}
