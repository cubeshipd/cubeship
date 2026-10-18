"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { api } from "@/lib/api";
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
  const [error, setError] = useState<string | null>(null);

  const installationID = Number(params.get("installation_id") ?? 0);
  // The App asks for OAuth on install, so GitHub sends this back beside
  // the id. It is the whole difference between "an installation" and
  // "your installation": the App is public, so anyone can install it and
  // any id is somebody's real id.
  const code = params.get("code") ?? "";

  useEffect(() => {
    if (!installationID) {
      setError("GitHub sent you back without an installation id, so there is nothing to record.");
      return;
    }
    if (!code) {
      setError(
        "GitHub sent you back without the code that proves this installation is yours. " +
          "That happens when the App was registered before it asked for it — re-register this " +
          "instance's App from the Instance page and install it again.",
      );
      return;
    }

    api
      .post<void>(`/github`, {
        installation_id: installationID,
        code,
      })
      .then(() => window.location.replace("/git"))
      .catch((e) => setError(message(e)));
  }, [installationID, code]);

  if (error) return <ErrorAlert error={error} />;
  return <p className="text-sm text-muted-foreground">Recording the installation…</p>;
}
