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
  const [loadingBranches, setLoadingBranches] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // checked is "the daemon has answered", success or failure — not "the
  // answer was yes". Until it is true this screen does not know whether
  // the instance has a GitHub App, and the button for "it has none"
  // registers one, which breaks every installation on the App it
  // already had. So nothing that could be pressed is drawn before it.
  const [checked, setChecked] = useState(false);

  // One load, not three: what is on screen depends on all of these
  // together, and answering them separately meant the screen rendered
  // three times on the way to being right — the first of those being
  // the one with the dangerous button on it.
  //
  // Each carries its own catch so one failure does not take the others
  // with it: knowing the App is registered is still worth having when
  // the repository listing is the thing that broke.
  const loadState = useCallback(() => {
    setLoading(true);
    Promise.all([
      api.get<Settings>("/settings").catch(() => null),
      api.get<GitHubConnections>(`/github`).catch((e) => {
        setError(message(e));
        return null;
      }),
      api.get<GitHubRepository[]>(`/github/repositories`).catch(() => []),
    ])
      .then(([s, c, r]) => {
        if (s) setSettings(s);
        if (c) {
          setConnections(c);
          setError(null);
        }
        setRepos(r ?? []);
      })
      .finally(() => {
        setLoading(false);
        setChecked(true);
      });
  }, []);
  useEffect(loadState, [loadState]);

  // Connecting happens on GitHub, in another tab. Coming back to this
  // one is the only signal there is that it may have finished, and it
  // is a better one than a button asking someone to say so.
  useEffect(() => {
    const recheck = () => {
      if (document.visibilityState === "visible") loadState();
    };
    window.addEventListener("visibilitychange", recheck);
    return () => window.removeEventListener("visibilitychange", recheck);
  }, [loadState]);

  // fullName is how GitHub names a repository and how the branch listing
  // asks for it; the app stores a URL.
  const fullName = repo.replace(/^https?:\/\/(www\.)?github\.com\//i, "").replace(/\.git$/, "");

  // A repository is `owner/name`; anything without a slash is not one
  // yet — nothing chosen, or a URL this screen could not take apart.
  //
  // The test used to be the other way round, which meant a real
  // repository cleared the branches and never asked for any, and an
  // empty field asked the daemon for the branches of "". Every branch
  // list on this screen was therefore the empty one, and it said so in
  // the words for a repository the App cannot read.
  useEffect(() => {
    if (!fullName.includes("/")) {
      setBranches(null);
      return;
    }
    setLoadingBranches(true);
    // Picking one repository and then another lands two answers, and
    // not necessarily in that order.
    let live = true;
    api
      .get<GitHubBranch[]>(`/github/branches?repo=${encodeURIComponent(fullName)}`)
      .then((found) => {
        if (live) setBranches(found ?? []);
      })
      .catch(() => {
        // An empty list rather than null: the select's own message
        // covers both readings, and null would leave it saying
        // "choose a repository first" for one that is chosen.
        if (live) setBranches([]);
      })
      .finally(() => {
        if (live) setLoadingBranches(false);
      });
    return () => {
      live = false;
    };
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

  if (!checked || !connected) {
    return (
      <div className="space-y-3">
        {checked && <ErrorAlert error={error} />}
        <GitProviders
          providers={providers}
          canRegister={me.role === "admin"}
          returnTo={returnTo}
          checking={!checked}
        />
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
          busy={loadingBranches}
          disabled={!fullName}
          choices={branchChoices}
          value={gitRef}
          onChange={onRef}
        />
      </div>
    </div>
  );
}
