"use client";

import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { InstanceDomain } from "@/components/instance-domain";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Instance() {
  return (
    <>
      <PageHeader title="Instance" />
      <Body />
    </>
  );
}

function Body() {
  const [current, setCurrent] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setCurrent)
      .catch((e) => setError(message(e)));
  }, []);

  if (!current) return <ErrorAlert error={error} />;

  return (
    <>
      <ErrorAlert error={error} />

      {!current.tls_enabled && (
        <Notice tone="warning">
          No certificates yet. Pick where this instance lives below — with a DNS provider connected,
          that is the whole of it: the records are written and the certificates follow.
        </Notice>
      )}

      <SectionHeader
        title="Domain"
        sub="The instance's own name. The dashboard and the API are served at it, the registry at registry.<domain>, and anything Cubeship grows later underneath — which is why a subdomain you hand over whole beats your apex."
      />
      <InstanceDomain settings={current} onSaved={setCurrent} />
    </>
  );
}
