"use client";

import { useEffect, useState } from "react";
import { api, type Settings } from "@/lib/api";
import { Button, Card, ErrorNote, Field, PageHeader, Shell, inputClass, message } from "@/components/ui";

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

  if (!current) return <ErrorNote error={error} />;

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
      <ErrorNote error={error} />
      {!current.tls_enabled && (
        <div className="mb-3.5 rounded-md border border-warn/45 bg-warn/10 px-3 py-2.5 text-sm">
          No certificates yet. Both a domain and a contact address are needed, and{" "}
          <span className="font-mono text-xs">api.{domain || "<domain>"}</span> and{" "}
          <span className="font-mono text-xs">registry.{domain || "<domain>"}</span> must resolve to
          this host.
        </div>
      )}

      <Card>
        <form onSubmit={save}>
          <Field label="Domain" hint="Apps and the API are served under it.">
            <input
              className={inputClass}
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              placeholder="example.com"
            />
          </Field>
          <Field label="Let's Encrypt contact address" hint="Where expiry warnings go.">
            <input
              className={inputClass}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@example.com"
            />
          </Field>
          <div className="flex items-center gap-3">
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? "Applying…" : "Save"}
            </Button>
            {saved && (
              <span className="text-xs text-muted">
                Saved. Apps already running keep the routing they were deployed with — redeploy them
                to serve over HTTPS.
              </span>
            )}
          </div>
        </form>
      </Card>

      {current.registry_host && (
        <Card>
          <div className="text-xs text-muted">Push images to</div>
          <div className="mt-1 font-mono text-sm">{current.registry_host}</div>
        </Card>
      )}
    </>
  );
}
