"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

// Where GitHub sends someone back after creating the App from a
// manifest. It arrives with a code, and the code is the credential: it
// is single-use, expires in an hour, and is spent immediately.
export default function GitHubAppCreated() {
  return (
    <Suspense>
      <Exchange />
    </Suspense>
  );
}

function Exchange() {
  const params = useSearchParams();
  const router = useRouter();
  const code = params.get("code") ?? "";
  // What GitHub echoed back from the manifest form. The daemon issued
  // it when the flow started and refuses a code that returns without
  // it: the code alone is only evidence that somebody, somewhere, made
  // an App, and this page is reached by following a link with a session
  // cookie attached.
  const manifestState = params.get("state") ?? "";
  // Where the flow was started from. Coming back to it is the
  // difference between "the App exists" and "carry on with what you
  // were doing".
  const returnTo = params.get("return") ?? "";
  const [state, setState] = useState<"working" | "done" | "failed">("working");
  const [slug, setSlug] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!code) {
      setError("GitHub sent you back without a code, so there is nothing to exchange.");
      setState("failed");
      return;
    }
    api
      .post<Settings>("/settings/github/manifest", { code, state: manifestState })
      .then((s) => {
        setSlug(s.github_app_slug ?? "");
        setState("done");

        // Straight on to installing it. Registering the App is
        // machinery — nobody sets out to create a GitHub App, they set
        // out to connect their code — so this is the middle of one
        // flow rather than the end of a step. Landing back on the
        // dashboard here would make someone press a second button to
        // finish what they already asked for.
        if (s.github_app_slug) {
          window.location.replace(`https://github.com/apps/${s.github_app_slug}/installations/new`);
          return;
        }
        if (returnTo.startsWith("/")) {
          // Only a path from this dashboard: a full URL here would be
          // an open redirect wearing a query parameter.
          router.replace(returnTo);
        }
      })
      .catch((e) => {
        setError(message(e));
        setState("failed");
      });
  }, [code, manifestState, returnTo, router]);

  return (
    <>
      <PageHeader title="Registering the GitHub App" />
      {state === "working" && (
        <p className="text-sm text-muted-foreground">Exchanging the code with GitHub…</p>
      )}
      {state === "done" && (
        <Notice>
          This instance is now the GitHub App <code>{slug}</code>. Install it on the accounts you
          deploy from — an app&apos;s{" "}
          <Link href="/" className="underline underline-offset-4">
            settings
          </Link>{" "}
          will offer it.
        </Notice>
      )}
      {state === "failed" && <ErrorAlert error={error} />}
    </>
  );
}
