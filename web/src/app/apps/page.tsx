"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { api, type App, type Deployment, type EnvView } from "@/lib/api";
import { Button, Card, ErrorNote, Field, Shell, Status, inputClass, message } from "@/components/ui";

// One app, identified by its reference in the query string. A static
// export has no dynamic segments, and the reference is four path
// components anyway — carrying it as one value keeps it whole.
export default function AppPage() {
  return (
    <Shell>
      <Suspense>
        <Detail />
      </Suspense>
    </Shell>
  );
}

function Detail() {
  const router = useRouter();
  const reference = useSearchParams().get("ref") ?? "";
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}`;
  const reload = useCallback(() => {
    if (!reference) return;
    api.get<App>(path).then(setApp).catch((e) => setError(message(e)));
  }, [path, reference]);
  useEffect(reload, [reload]);

  if (!reference) return <p className="text-sm text-muted">No app named. <Link href="/">Back to apps</Link>.</p>;
  if (error) return <ErrorNote error={error} />;
  if (!app) return null;

  return (
    <>
      <header className="mb-6 flex items-start justify-between">
        <div>
          <h1 className="font-mono text-lg">{app.reference}</h1>
          <p className="mt-1 text-sm text-muted">
            {app.domain} · <Status value={app.status} />
          </p>
        </div>
        <Button
          variant="danger"
          onClick={async () => {
            try {
              await api.del(path);
              router.push("/");
            } catch (e) {
              setError(message(e));
            }
          }}
        >
          Delete app
        </Button>
      </header>

      {app.image ? (
        <Card>
          <div className="text-xs text-muted">Push an image here and it deploys</div>
          <div className="mt-1 font-mono text-sm break-all">docker push {app.image}:latest</div>
        </Card>
      ) : (
        <div className="mb-3.5 rounded-md border border-warn/45 bg-warn/10 px-3 py-2.5 text-sm">
          There is nowhere to push yet — this instance has no domain. Set one under{" "}
          <Link href="/settings">Instance</Link>.
        </div>
      )}

      <Deployments reference={reference} onDeployed={reload} />
      <EnvVars reference={reference} />
      <Logs reference={reference} />
    </>
  );
}

function Deployments({ reference, onDeployed }: { reference: string; onDeployed: () => void }) {
  const [list, setList] = useState<Deployment[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/deployments`;
  const reload = useCallback(() => {
    api.get<Deployment[]>(path).then(setList).catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // A deploy runs detached, so the row is the only place its outcome
  // appears. Poll while one is in flight and stop when none is.
  useEffect(() => {
    if (!list?.some((d) => d.status === "pending" || d.status === "deploying")) return;
    const timer = setInterval(() => {
      reload();
      onDeployed();
    }, 2000);
    return () => clearInterval(timer);
  }, [list, reload, onDeployed]);

  return (
    <>
      <div className="mt-7 mb-2.5 flex items-center justify-between">
        <h2 className="text-[15px] font-medium">Deployments</h2>
        <Button
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setError(null);
            try {
              await api.post(`/apps/${reference}/deploy`);
              reload();
            } catch (e) {
              setError(message(e));
            }
            setBusy(false);
          }}
        >
          Redeploy latest
        </Button>
      </div>
      <ErrorNote error={error} />
      <Card className="p-0">
        <table className="w-full text-sm">
          <tbody>
            {list?.map((d) => (
              <tr key={d.id} className="border-b border-line last:border-0">
                <td className="p-3">
                  <Status value={d.status} />
                </td>
                <td className="p-3 font-mono text-xs text-muted break-all">{d.image}</td>
                <td className="p-3 text-xs text-muted whitespace-nowrap">
                  {new Date(d.created_at).toLocaleString()}
                </td>
                {d.error && <td className="p-3 text-xs text-bad">{d.error}</td>}
              </tr>
            ))}
            {list?.length === 0 && (
              <tr>
                <td className="p-3 text-sm text-muted">Nothing deployed yet.</td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>
    </>
  );
}

function EnvVars({ reference }: { reference: string }) {
  const [view, setView] = useState<EnvView | null>(null);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/env`;
  const reload = useCallback(() => {
    api.get<EnvView>(path).then(setView).catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  return (
    <>
      <h2 className="mt-7 mb-2.5 text-[15px] font-medium">Environment</h2>
      <p className="mb-2.5 text-sm text-muted">
        What the container runs with: the project&apos;s variables, then the environment&apos;s, then
        the app&apos;s own. Setting one here overrides the levels above it.
      </p>
      <ErrorNote error={error} />
      <Card className="p-0">
        <table className="w-full text-sm">
          <tbody>
            {view?.effective?.map((v) => (
              <tr key={v.key} className="border-b border-line last:border-0">
                <td className="p-3 font-mono text-xs">{v.key}</td>
                <td className="p-3 font-mono text-xs break-all">{v.value}</td>
                <td className="p-3 text-right text-xs text-muted">
                  {v.source}
                  {v.source === "app" && (
                    <button
                      className="ml-3 hover:text-bad"
                      onClick={async () => {
                        try {
                          await api.patch(path, { unset: [v.key] });
                          reload();
                        } catch (e) {
                          setError(message(e));
                        }
                      }}
                    >
                      Unset
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {view?.effective?.length === 0 && (
              <tr>
                <td className="p-3 text-sm text-muted">No variables.</td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>
      <form
        className="flex items-end gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          setError(null);
          try {
            await api.patch(path, { set: { [key]: value } });
            setKey("");
            setValue("");
            reload();
          } catch (err) {
            setError(message(err));
          }
        }}
      >
        <div className="flex-1">
          <Field label="Key">
            <input className={inputClass} value={key} onChange={(e) => setKey(e.target.value)} />
          </Field>
        </div>
        <div className="flex-[2]">
          <Field label="Value">
            <input className={inputClass} value={value} onChange={(e) => setValue(e.target.value)} />
          </Field>
        </div>
        <Button type="submit" className="mb-3">
          Set
        </Button>
      </form>
      <p className="text-xs text-muted">Takes effect on the next deploy.</p>
    </>
  );
}

function Logs({ reference }: { reference: string }) {
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Logs come back as plain text, not JSON, so they bypass the client.
  const load = useCallback(async () => {
    setError(null);
    try {
      const res = await fetch(`/api/apps/${reference}/logs?tail=200`, { credentials: "same-origin" });
      const body = await res.text();
      if (!res.ok) throw new Error(body.trim() || res.statusText);
      setText(body);
    } catch (e) {
      setError(message(e));
    }
  }, [reference]);

  return (
    <>
      <div className="mt-7 mb-2.5 flex items-center justify-between">
        <h2 className="text-[15px] font-medium">Logs</h2>
        <Button onClick={load}>{text === null ? "Load" : "Refresh"}</Button>
      </div>
      <ErrorNote error={error} />
      {text !== null && (
        <pre className="max-h-[420px] overflow-auto rounded-md border border-line bg-black/40 p-3 text-xs whitespace-pre-wrap break-all">
          {text || "(no output)"}
        </pre>
      )}
    </>
  );
}
