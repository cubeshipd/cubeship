"use client";

import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { InstanceDomain } from "@/components/instance-domain";
import { InstanceMetrics } from "@/components/instance-metrics";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Instance() {
  return (
    <>
      <PageHeader title="Instance" />
      {/* What the box is doing comes first, for the same reason it does
          on an app's page and a database's: it is the question somebody
          has before they know they have one. It is outside Body, which
          waits on the settings — the machine's chart has nothing to do
          with them, and a screen that blanks until an unrelated request
          lands reads as slow however fast that request was. */}
      <InstanceMetrics />
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
