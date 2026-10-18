"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon, GitBranchIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { BitbucketConnectDialog } from "@/components/bitbucket-connect-dialog";
import { BitbucketWebhookCard } from "@/components/bitbucket-webhook-card";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GitRemoteConnectDialog } from "@/components/git-remote-connect-dialog";
import { GitRemoteWebhookDialog } from "@/components/git-remote-webhook-dialog";
import { GitHubAppCard } from "@/components/github-app-card";
import { ConnectGitHub } from "@/components/github-connect";
import { GiteaIcon, GitHubIcon, GitLabIcon } from "@/components/icons";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { api, type GitHubConnections, type GitRemoteConnection, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

// One row per provider.
//
// A connection is a whole relationship with a provider, not one account
// inside it: connecting GitHub once and then adding a second
// organization to it is *configuring* that connection, not making
// another. So the accounts it covers are a column, and both actions —
// widen it, or end it — act on the connection.
type Provider = {
  id: "github" | "bitbucket" | "gitlab" | "gitea";
  name: string;
  icon: typeof GitHubIcon | typeof GitBranchIcon;
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
  const [disconnecting, setDisconnecting] = useState<"github" | "bitbucket" | null>(null);
  const [disconnectRemote, setDisconnectRemote] = useState<GitRemoteConnection | null>(null);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });

  const connections = useQuery({
    queryKey: ["github"],
    queryFn: () => api.get<GitHubConnections>(`/github`),
  });
  const bitbucket = useQuery({
    queryKey: ["bitbucket"],
    queryFn: () => api.get<{ id: number; account: string }[]>(`/bitbucket`),
  });
  const remotes = useQuery({
    queryKey: ["gitremotes"],
    queryFn: () => api.get<GitRemoteConnection[]>("/gitremotes"),
  });

  const slug = settings.data?.github_app_slug ?? "";
  const installations = connections.data?.installations ?? [];

  const providers: Provider[] | null =
    settings.isLoading || connections.isLoading || bitbucket.isLoading || remotes.isLoading
      ? null
      : [
          {
            id: "github",
            name: "GitHub",
            icon: GitHubIcon,
            connected:
              installations.length > 0 &&
              connections.data?.app_status === "available" &&
              settings.data?.github_oauth_ready !== false,
            accounts: installations.map((i) => i.account),
            // `installations/new` is the App's own entry point: for an
            // account that already has it, GitHub shows the configure
            // page instead of starting again — which is the same door
            // for "add an organization" and "let it see one more repo".
            configureURL:
              connections.data?.app_status === "available" && slug
                ? `https://github.com/apps/${slug}/installations/new`
                : "",
          },
          {
            id: "bitbucket",
            name: "Bitbucket",
            icon: GitBranchIcon,
            connected: (bitbucket.data?.length ?? 0) > 0,
            accounts: (bitbucket.data ?? []).map((c) => c.account),
            configureURL: "",
          },
          ...(["gitlab", "gitea"] as const).map((id) => ({
            id,
            name: id === "gitlab" ? "GitLab" : "Gitea",
            icon: id === "gitlab" ? GitLabIcon : GiteaIcon,
            connected: (remotes.data ?? []).some((c) => c.provider === id),
            accounts: (remotes.data ?? [])
              .filter((c) => c.provider === id)
              .map((c) => `${c.account} · ${c.base_url}`),
            configureURL: "",
          })),
        ];

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
        p.id === "gitlab" || p.id === "gitea" ? (
          <div className="flex justify-end">
            <GitRemoteConnectDialog
              provider={p.id}
              connected={p.connected}
              onConnected={() => void remotes.refetch()}
            />
          </div>
        ) : p.connected ? (
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
              onClick={() => setDisconnecting(p.id as "github" | "bitbucket")}
            />
          </RowActions>
        ) : (
          <div className="flex justify-end">
            {p.id === "github" ? (
              <ConnectGitHub
                settings={settings.data}
                instanceName={settings.data?.domain ?? ""}
                size="sm"
                appStatus={connections.data?.app_status}
                onRetry={() => void connections.refetch()}
              />
            ) : (
              <BitbucketConnectDialog onConnected={() => void bitbucket.refetch()} />
            )}
          </div>
        ),
    },
  ];

  return (
    <>
      <SectionHeader
        page
        title="Git providers"
        sub="Connect source providers for private builds and deploys on push."
      />
      <ErrorAlert
        error={
          connections.error
            ? message(connections.error)
            : bitbucket.error
              ? message(bitbucket.error)
              : remotes.error
                ? message(remotes.error)
                : null
        }
      />

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

      {(remotes.data?.length ?? 0) > 0 && (
        <div className="mt-8 space-y-4">
          <SectionHeader title="Connected instances" />
          <DataTable
            columns={[
              {
                id: "provider",
                header: "Provider",
                width: 18,
                cell: (c: GitRemoteConnection) => (c.provider === "gitlab" ? "GitLab" : "Gitea"),
              },
              {
                id: "instance",
                header: "Instance",
                width: 42,
                cell: (c: GitRemoteConnection) => (
                  <span className="font-mono text-xs">{c.base_url}</span>
                ),
              },
              {
                id: "account",
                header: "Account",
                width: 22,
                cell: (c: GitRemoteConnection) => c.account,
              },
              {
                id: "actions",
                header: "",
                width: 18,
                align: "right" as const,
                cell: (c: GitRemoteConnection) => (
                  <div className="flex items-center justify-end gap-2">
                    <GitRemoteWebhookDialog connection={c} />
                    <button
                      type="button"
                      className="inline-flex size-9 items-center justify-center text-muted-foreground hover:text-danger"
                      aria-label={`Disconnect ${c.account} from ${c.base_url}`}
                      onClick={() => setDisconnectRemote(c)}
                    >
                      <Trash2Icon className="size-4" />
                    </button>
                  </div>
                ),
              },
            ]}
            rows={remotes.data ?? []}
            rowKey={(c) => String(c.id)}
          />
        </div>
      )}

      {/* The escape hatch, and deliberately a link rather than a
          section: an App made by hand is a real thing to have and not
          something anyone should read about on the way to connecting. */}
      {me.role === "admin" && !settings.data?.github_connected && settings.data && (
        <div className="mt-4">
          <GitHubAppCard settings={settings.data} onSaved={() => settings.refetch()} />
        </div>
      )}

      {me.role === "admin" &&
        (bitbucket.data?.length ?? 0) > 0 &&
        !settings.data?.bitbucket_webhook_ready && (
          <BitbucketWebhookCard onSaved={() => void settings.refetch()} />
        )}

      <ConfirmDialog
        open={disconnectRemote !== null}
        onOpenChange={(open) => {
          if (!open) setDisconnectRemote(null);
        }}
        title={`Disconnect ${disconnectRemote?.account ?? "this account"}?`}
        description="Private builds and push deployments from this connection will stop. Running containers keep serving."
        confirmWord="disconnect"
        confirmLabel="Disconnect"
        onConfirm={async () => {
          if (!disconnectRemote) return;
          await api.del(`/gitremotes/${disconnectRemote.id}`);
          setDisconnectRemote(null);
          await remotes.refetch();
        }}
      />

      <ConfirmDialog
        open={disconnecting !== null}
        onOpenChange={(open) => {
          if (!open) setDisconnecting(null);
        }}
        title={
          disconnecting === "bitbucket"
            ? "Disconnect Bitbucket?"
            : installations.length > 1
              ? `Disconnect GitHub, and all ${installations.length} accounts with it?`
              : "Disconnect GitHub?"
        }
        confirmWord="disconnect"
        description={
          disconnecting === "bitbucket" ? (
            <>
              Private Bitbucket repositories stop being available for new builds, and pushes stop
              deploying apps. Containers already running keep serving.
            </>
          ) : (
            <>
              The App stays installed on GitHub — this only stops Cubeship using it. Apps built from
              private repositories on{" "}
              <code>{installations.map((i) => i.account).join(", ") || "those accounts"}</code> stop
              being able to clone, and a push to one stops deploying anything. Containers already
              running keep serving.
            </>
          )
        }
        confirmLabel="Disconnect"
        onConfirm={async () => {
          // Every installation, because the connection is the thing
          // being ended — leaving one behind would be a connection the
          // page says is gone.
          if (disconnecting === "bitbucket") {
            for (const connection of bitbucket.data ?? [])
              await api.del(`/bitbucket/${connection.id}`);
            await bitbucket.refetch();
          } else {
            for (const i of installations) await api.del(`/github/${i.id}`);
            await connections.refetch();
          }
          setDisconnecting(null);
        }}
      />
    </>
  );
}
