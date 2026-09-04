"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { ValueCard } from "@/components/value-card";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Instance() {
  return (
    <Shell>
      <PageHeader
        title="Instance"
        sub="Set these once the box has a domain pointing at it. Until then apps are served over plain HTTP and there is nowhere to push."
      />
      <Form />
    </Shell>
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
          <code>api.{domain || "<domain>"}</code> and <code>registry.{domain || "<domain>"}</code>{" "}
          must resolve to this host.
        </Notice>
      )}

      <Card className="mb-4">
        <CardContent>
          <form onSubmit={save} className="space-y-4">
            <TextField
              label="Domain"
              hint="Apps and the API are served under it."
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              placeholder="example.com"
              spellCheck={false}
              className="font-mono"
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

      {current.registry_host && <ValueCard label="Push images to" value={current.registry_host} />}
    </>
  );
}
