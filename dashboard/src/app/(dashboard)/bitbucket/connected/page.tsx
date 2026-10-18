"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { api } from "@/lib/api";
import { message } from "@/lib/errors";

export default function BitbucketConnected() {
  return (
    <Suspense>
      <Callback />
    </Suspense>
  );
}

function Callback() {
  const params = useSearchParams();
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const code = params.get("code") ?? "";
    const state = params.get("state") ?? "";
    if (!code || !state) {
      setError("Bitbucket did not return the OAuth code and state needed to connect this account.");
      return;
    }
    api
      .post<void>("/bitbucket", { code, state })
      .then(() => window.location.replace("/git"))
      .catch((e) => setError(message(e)));
  }, [params]);
  if (error) return <ErrorAlert error={error} />;
  return <p className="text-sm text-muted-foreground">Connecting Bitbucket…</p>;
}
