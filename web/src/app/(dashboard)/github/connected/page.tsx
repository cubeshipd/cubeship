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

  useEffect(() => {
    if (!org) return;
    if (!installationID) {
      setError("GitHub sent you back without an installation id, so there is nothing to record.");
      setState("failed");
      return;
    }

    // GitHub does not send the account it landed on, only the id. The
    // daemon asks GitHub for the rest, so an empty account here is the
    // App's own answer rather than something to guess at.
    api
      .post<GitHubInstallation>(`/orgs/${org}/github`, {
        installation_id: installationID,
        account: params.get("account") ?? "",
      })
      .then((created) => {
        setAccount(created.account);
        setState("done");
      })
      .catch((e) => {
        setError(message(e));
        setState("failed");
      });
  }, [org, installationID, params]);

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
