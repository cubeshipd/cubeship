"use client";

import { GitBranchIcon } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect, type GlideSelectOption } from "@/components/glide-select";
import { GiteaIcon, GitHubIcon, GitLabIcon } from "@/components/icons";
import { ApiError, api, type GitBranch, type GitRepository } from "@/lib/api";
import { message } from "@/lib/errors";

type RepositoryChoice = GitRepository & {
  provider: "github" | "bitbucket" | "gitlab" | "gitea";
  url: string;
};

const comparableURL = (url: string) => url.replace(/\.git\/?$/i, "").replace(/\/$/, "");

export function GitSource({
  sourceControl,
  buildControl,
  repo,
  gitRef,
  onRepo,
  onRef,
}: {
  sourceControl: ReactNode;
  buildControl: ReactNode;
  repo: string;
  gitRef: string;
  onRepo: (url: string, defaultBranch: string) => void;
  onRef: (ref: string) => void;
}) {
  const [repositories, setRepositories] = useState<RepositoryChoice[]>([]);
  const [branches, setBranches] = useState<GitBranch[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingBranches, setLoadingBranches] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadRepositories = useCallback(() => {
    setLoading(true);
    Promise.allSettled([
      api.get<GitRepository[]>("/github/repositories"),
      api.get<GitRepository[]>("/bitbucket/repositories"),
      api.get<GitRepository[]>("/gitremotes/repositories"),
    ])
      .then(([github, bitbucket, remotes]) => {
        const from = (
          result: PromiseSettledResult<GitRepository[]>,
          provider: "github" | "bitbucket",
        ) =>
          result.status === "fulfilled"
            ? result.value.map((entry) => ({
                ...entry,
                provider,
                url: `https://${provider === "github" ? "github.com" : "bitbucket.org"}/${entry.full_name}`,
              }))
            : [];
        const remoteRepos =
          remotes.status === "fulfilled"
            ? remotes.value.filter(
                (entry): entry is RepositoryChoice =>
                  !!entry.url && (entry.provider === "gitlab" || entry.provider === "gitea"),
              )
            : [];
        setRepositories([
          ...from(github, "github"),
          ...from(bitbucket, "bitbucket"),
          ...remoteRepos,
        ]);
        const failure = [
          github.status === "rejected" &&
          (!(github.reason instanceof ApiError) || github.reason.status !== 409)
            ? github.reason
            : null,
          bitbucket.status === "rejected" ? bitbucket.reason : null,
          remotes.status === "rejected" ? remotes.reason : null,
        ].find(Boolean);
        setError(failure ? message(failure) : null);
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(loadRepositories, [loadRepositories]);
  useEffect(() => {
    const recheck = () => {
      if (document.visibilityState === "visible") loadRepositories();
    };
    window.addEventListener("visibilitychange", recheck);
    return () => window.removeEventListener("visibilitychange", recheck);
  }, [loadRepositories]);

  const selected = repositories.find((entry) => comparableURL(entry.url) === comparableURL(repo));
  const match = repo.match(
    /^https?:\/\/(?:www\.)?(github\.com|bitbucket\.org)\/(.+?)(?:\.git)?\/?$/i,
  );
  const provider =
    selected?.provider ?? (match?.[1]?.toLowerCase() === "bitbucket.org" ? "bitbucket" : "github");
  const fullName = selected?.full_name ?? match?.[2] ?? "";
  const selectedRepo = selected?.url ?? (match ? comparableURL(repo) : repo);

  useEffect(() => {
    if ((!selected && !match) || !fullName.includes("/")) {
      setBranches([]);
      return;
    }
    let live = true;
    setLoadingBranches(true);
    api
      .get<GitBranch[]>(
        provider === "gitlab" || provider === "gitea"
          ? `/gitremotes/branches?repo=${encodeURIComponent(selectedRepo)}`
          : `/${provider}/branches?repo=${encodeURIComponent(fullName)}`,
      )
      .then((found) => {
        if (live) setBranches(found ?? []);
      })
      .catch(() => {
        if (live) setBranches([]);
      })
      .finally(() => {
        if (live) setLoadingBranches(false);
      });
    return () => {
      live = false;
    };
  }, [match?.[0], fullName, provider, selectedRepo, selected]);

  const repoChoices: GlideSelectOption[] = repositories.map((entry) => ({
    value: entry.url,
    label: (
      <span className="inline-flex min-w-0 items-center gap-2">
        {entry.provider === "github" ? (
          <GitHubIcon className="size-4 shrink-0" />
        ) : entry.provider === "gitlab" ? (
          <GitLabIcon className="size-4 shrink-0" />
        ) : entry.provider === "gitea" ? (
          <GiteaIcon className="size-4 shrink-0" />
        ) : (
          <GitBranchIcon className="size-4 shrink-0" />
        )}
        <span className="truncate">{entry.full_name}</span>
        <span className="text-xs text-muted-foreground">
          {entry.provider === "github"
            ? "GitHub"
            : entry.provider === "bitbucket"
              ? "Bitbucket"
              : new URL(entry.url).host}
        </span>
      </span>
    ),
    searchText: `${entry.provider} ${entry.full_name}`,
    tag: entry.private ? "private" : undefined,
  }));
  if (selectedRepo && !repoChoices.some((choice) => choice.value === selectedRepo)) {
    repoChoices.unshift({
      value: selectedRepo,
      label: fullName,
      tag: "unavailable",
      searchText: `${provider} ${fullName}`,
    });
  }

  const branchChoices: GlideSelectOption[] = [
    { value: "", label: "Any branch" },
    ...(branches ?? []).map((branch) => ({ value: branch.name, label: branch.name })),
  ];
  if (gitRef && !branchChoices.some((choice) => choice.value === gitRef)) {
    branchChoices.push({ value: gitRef, label: gitRef, tag: "current" });
  }

  return (
    <div className="space-y-4">
      <ErrorAlert error={error} />
      <div className="grid grid-cols-1 items-start gap-4 sm:grid-cols-2">
        {sourceControl}
        <GlideSelect
          label="Repository"
          placeholder="Choose a repository"
          emptyMessage="No accessible repositories were found."
          busy={loading}
          options={repoChoices}
          value={selectedRepo}
          onChange={(picked) => {
            const chosen = repositories.find((entry) => entry.url === picked);
            onRepo(picked, chosen?.default_branch ?? (picked === selectedRepo ? gitRef : ""));
          }}
          searchable
          className="w-full"
          triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        />
        <GlideSelect
          label="Branch"
          placeholder={selectedRepo ? "Choose a branch" : "Choose a repository first"}
          emptyMessage="No branches found for this repository."
          busy={loadingBranches}
          disabled={!selectedRepo}
          options={branchChoices}
          value={gitRef}
          onChange={onRef}
          searchable
          className="w-full"
          triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:cursor-not-allowed disabled:opacity-50"
        />
        {buildControl}
      </div>
      {!loading && repositories.length === 0 && (
        <a
          href="/git"
          className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
        >
          Connect a Git provider to choose a repository
        </a>
      )}
    </div>
  );
}
