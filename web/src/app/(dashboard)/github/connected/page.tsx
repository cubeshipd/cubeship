"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { api, type GitHubInstallation } from "@/lib/api";
import { message } from "@/lib/errors";

// Where GitHub sends someone back after they install the App. It
// arrives with the installation's id in the query, and this is what
// ties it to an organization — until it does, the installation exists
// on GitHub and means nothing here.
export default function GitHubConnected() {
  return (
    <Suspense>
      <Landing />
    </Suspense>
  );
}

function Landing() {
  const params = useSearchParams();
  const { org } = useOrg();
  const [state, setState] = useState<"working" | "done" | "failed">("working");
  const [error, setError] = useState<string | null>(null);
  const [account, setAccount] = useState("");

  const installationID = Number(params.get("installation_id") ?? 0);
  // The App asks for OAuth on install, so GitHub sends this back beside
  // the id. It is the whole difference between "an installation" and
  // "your installation": the App is public, so anyone can install it and
  // any id is somebody's real id.
  const code = params.get("code") ?? "";

  useEffect(() => {
    if (!org) return;
    if (!installationID) {
      setError("GitHub sent you back without an installation id, so there is nothing to record.");
      setState("failed");
      return;
    }
    if (!code) {
      setError(
        "GitHub sent you back without the code that proves this installation is yours. " +
          "That happens when the App was registered before it asked for it — re-register this " +
          "instance's App from the Instance page and install it again.",
      );
      setState("failed");
      return;
    }

    // The account is not sent: it comes back from the daemon, which
    // takes it from GitHub's own answer rather than from anything in
    // this URL.
    api
      .post<GitHubInstallation>(`/orgs/${org}/github`, {
        installation_id: installationID,
        code,
      })
      .then((created) => {
        setAccount(created.account);
        setState("done");
      })
      .catch((e) => {
        setError(message(e));
        setState("failed");
      });
  }, [org, installationID, code]);

  return (
    <>
      <PageHeader title="Connecting GitHub" />
      {state === "working" && (
        <p className="text-sm text-muted-foreground">Recording the installation…</p>
      )}
      {state === "done" && (
        <Notice>
          <code>{account}</code> is connected to <code>{org}</code>. Apps built from its
          repositories can now be cloned, and a push to one deploys them.{" "}
          <Link href="/" className="underline underline-offset-4">
            Back to projects
          </Link>
          .
        </Notice>
      )}
      {state === "failed" && <ErrorAlert error={error} />}
    </>
  );
}
