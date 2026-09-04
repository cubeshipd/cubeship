"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubAppCard } from "@/components/github-app-card";
import { InstanceDNS } from "@/components/instance-dns";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { ValueCard } from "@/components/value-card";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Instance() {
  return (
    <>
      <PageHeader
        title="Instance"
        sub="Set these once the box has a domain pointing at it. Until then apps are served over plain HTTP and there is nowhere to push."
      />
      <Form />
    </>
  );
}

function Form() {
  const [current, setCurrent] = useState<Settings | null>(null);
  const [domain, setDomain] = useState("");
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then((s) => {
        setCurrent(s);
        setDomain(s.domain);
        setEmail(s.acme_email);
      })
      .catch((e) => setError(message(e)));
  }, []);

  if (!current) return <ErrorAlert error={error} />;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      setCurrent(await api.put<Settings>("/settings", { domain, acme_email: email }));
      setSaved(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <ErrorAlert error={error} />

      {!current.tls_enabled && (
        <Notice tone="warning">
          No certificates yet. Both a domain and a contact address are needed, and{" "}
          <code>{domain || "<domain>"}</code> and <code>*.{domain || "<domain>"}</code> must resolve
          to this host — which the section below can arrange.
        </Notice>
      )}

      <SectionHeader
        title="Domain"
        sub="The instance's own name. The dashboard and the API are served at it, the registry at registry.<domain>, and anything Cubeship grows later underneath — which is why a subdomain you hand over whole beats your apex."
      />

      <Card className="mb-4">
        <CardContent>
          <form onSubmit={save} className="space-y-4">
            <TextField
              label="Domain"
              hint="A subdomain you give to Cubeship, e.g. cubeship.example.com. An apex works too — the recommendation is about how much DNS you have to keep touching, not about what is allowed."
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              placeholder="cubeship.example.com"
              spellCheck={false}
            />
            <TextField
              label="Let's Encrypt contact address"
              hint="Where expiry warnings go."
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@example.com"
              spellCheck={false}
            />
            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy}>
                {busy ? "Applying" : "Save"}
              </ActionButton>
              {saved && (
                <span className="text-xs text-muted-foreground">
                  Saved. Apps already running keep the routing they were deployed with — redeploy
                  them to serve over HTTPS.
                </span>
              )}
            </div>
          </form>
        </CardContent>
      </Card>

      <SectionHeader
        title="DNS"
        sub="Two records point the whole instance here: the name itself, and a wildcard under it for the registry and whatever comes after. Cubeship can write them through a connected provider, or you can copy them into your own."
      />
      <InstanceDNS settings={current} onSaved={setCurrent} />

      {current.registry_host && (
        <div className="mt-4">
          <ValueCard label="Push images to" value={current.registry_host} />
        </div>
      )}

      <GitHubAppCard settings={current} onSaved={setCurrent} />
    </>
  );
}
