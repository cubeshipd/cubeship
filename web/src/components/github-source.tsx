"use client";

import { useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { GitProviders } from "@/components/git-providers";
import { GitHubIcon } from "@/components/icons";
import { type Choice, SearchableSelect } from "@/components/searchable-select";
import { useSession } from "@/components/session-context";
import {
  api,
  type GitHubBranch,
  type GitHubConnections,
  type GitHubRepository,
  type Settings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// Choosing a repository and a branch, rather than typing a URL.
//
// Everything offered here is something this instance can actually clone:
// the list is what the App was granted, and the branches are that
// repository's. A URL field could name neither, and the failure would
// arrive minutes later inside a build.
//
// It is the first thing on the GitHub path because it gates the rest —
// there is no point choosing between Railpack and a Dockerfile for a
// repository Cubeship cannot reach.
export function GitHubSource({
  repo,
  gitRef,
  onRepo,
  onRef,
}: {
  repo: string;
  gitRef: string;
  onRepo: (url: string, defaultBranch: string) => void;
  onRef: (ref: string) => void;
}) {
  const me = useSession();
  // Creating the App from here should come back here, not to whatever
  // page GitHub redirected to.
  const returnTo =
    typeof window === "undefined" ? undefined : window.location.pathname + window.location.search;
  const [settings, setSettings] = useState<Settings | null>(null);
  const [connections, setConnections] = useState<GitHubConnections | null>(null);
  const [repos, setRepos] = useState<GitHubRepository[] | null>(null);
  const [branches, setBranches] = useState<GitHubBranch[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setSettings)
      .catch((e) => setError(message(e)));
  }, []);

  const loadRepos = useCallback(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      api.get<GitHubConnections>(`/github`),
      api.get<GitHubRepository[]>(`/github/repositories`).catch(() => []),
    ])
      .then(([c, r]) => {
        setConnections(c);
        setRepos(r ?? []);
      })
      .catch((e) => setError(message(e)))
      .finally(() => setLoading(false));
  }, []);
  useEffect(loadRepos, [loadRepos]);

  // Connecting happens on GitHub, in another tab. Coming back to this
  // one is the only signal there is that it may have finished, and it
  // is a better one than a button asking someone to say so.
  useEffect(() => {
    const recheck = () => {
      if (document.visibilityState === "visible") loadRepos();
    };
    window.addEventListener("visibilitychange", recheck);
    return () => window.removeEventListener("visibilitychange", recheck);
  }, [loadRepos]);

  // fullName is how GitHub names a repository and how the branch listing
  // asks for it; the app stores a URL.
  const fullName = repo.replace(/^https?:\/\/(www\.)?github\.com\//i, "").replace(/\.git$/, "");

  useEffect(() => {
    if (fullName.includes("/")) {
      setBranches(null);
      return;
    }
    api
      .get<GitHubBranch[]>(`/github/branches?repo=${encodeURIComponent(fullName)}`)
      .then(setBranches)
      .catch(() => setBranches(null));
  }, [fullName]);

  const connected = (connections?.installations.length ?? 0) > 0;

  // An instance that is not registered as a GitHub App has no install
  // page to send anyone to, so the button registers it instead.
  const providers = [
    {
      id: "github",
      name: "GitHub",
      icon: GitHubIcon,
      connected,
      href: settings?.github_connected ? (connections?.install_url ?? "") : "",
    },
  ];

  if (!connected) {
    return (
      <div className="space-y-3">
        <ErrorAlert error={error} />
        <GitProviders providers={providers} canRegister={me.is_super_admin} returnTo={returnTo} />
      </div>
    );
  }

  // The mark travels with the repository, not with the list. Today
  // every row here is GitHub's, but this list is what a second provider
  // would land in — and a list of names from two places, unmarked, is a
  // list where you cannot tell which `acme/api` you are picking.
  const repoChoices: Choice[] = (repos ?? []).map((r) => ({
    value: r.full_name,
    label: r.full_name,
    icon: GitHubIcon,
    hint: r.private ? "private" : undefined,
  }));

  const branchChoices: Choice[] = (branches ?? []).map((b) => ({
    value: b.name,
    label: b.name,
  }));

  return (
    <div className="space-y-4">
      <ErrorAlert error={error} />

      {/* Side by side, because they are one decision: a branch only
          means anything inside a repository, and stacked they read as
          two independent questions with the answer to the second
          waiting on the first. */}
      <div className="grid grid-cols-2 items-start gap-4">
        <SearchableSelect
          label="Repository"
          hint="What the App was granted. Install it on more from GitHub if one is missing."
          placeholder="Choose a repository"
          empty="The App is connected but was granted no repositories."
          busy={loading}
          choices={repoChoices}
          value={fullName}
          onChange={(picked) => {
            const chosen = repos?.find((r) => r.full_name === picked);
            onRepo(`https://github.com/${picked}`, chosen?.default_branch ?? "");
          }}
        />

        <SearchableSelect
          label="Branch"
          hint="What a deploy builds when it names nothing else. Leave it and a push to any branch deploys."
          placeholder={fullName ? "Choose a branch" : "Choose a repository first"}
          empty="No branches — or the App cannot read this repository."
          disabled={!fullName}
          choices={branchChoices}
          value={gitRef}
          onChange={onRef}
        />
      </div>
    </div>
  );
}
