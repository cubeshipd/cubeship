"use client";

import { useCallback, useEffect, useState } from "react";
import { api, type Org, type RegistryCredential } from "@/lib/api";
import { Button, Card, ErrorNote, Field, PageHeader, Shell, inputClass, message } from "@/components/ui";

// Logins for registries Cubeship does not run. Cubeship's own registry
// needs none of this — it authenticates each user with their API key.
export default function Registries() {
  return (
    <Shell>
      <PageHeader
        title="Registries"
        sub="Logins for registries Cubeship does not run. An app with an external image pulls through whichever of these matches its registry."
      />
      <Body />
    </Shell>
  );
}

function Body() {
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [org, setOrg] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<Org[]>("/orgs")
      .then((o) => {
        setOrgs(o);
        setOrg((current) => current || o[0]?.slug || "");
      })
      .catch((e) => setError(message(e)));
  }, []);

  return (
    <>
      <ErrorNote error={error} />
      {orgs.length > 1 && (
        <Card>
          <Field label="Organization">
            <select className={inputClass} value={org} onChange={(e) => setOrg(e.target.value)}>
              {orgs.map((o) => (
                <option key={o.slug} value={o.slug}>
                  {o.slug}
                </option>
              ))}
            </select>
          </Field>
        </Card>
      )}
      {org && <List org={org} />}
    </>
  );
}

function List({ org }: { org: string }) {
  const [creds, setCreds] = useState<RegistryCredential[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rotating, setRotating] = useState<number | null>(null);

  const path = `/orgs/${org}/registries`;
  const reload = useCallback(() => {
    api.get<RegistryCredential[]>(path).then(setCreds).catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  return (
    <>
      <ErrorNote error={error} />
      <Card className="p-0">
        <table className="w-full text-sm">
          <tbody>
            {creds?.map((c) => (
              <tr key={c.id} className="border-b border-line last:border-0">
                <td className="p-3">{c.name}</td>
                <td className="p-3 font-mono text-xs text-muted">{c.host}</td>
                <td className="p-3 text-xs text-muted">{c.username}</td>
                <td className="p-3 text-right text-xs">
                  <button className="text-muted hover:text-body" onClick={() => setRotating(c.id)}>
                    Replace login
                  </button>
                  <button
                    className="ml-3 text-muted hover:text-bad"
                    onClick={async () => {
                      try {
                        await api.del(`${path}/${c.id}`);
                        reload();
                      } catch (e) {
                        setError(message(e));
                      }
                    }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {creds?.length === 0 && (
              <tr>
                <td className="p-3 text-sm text-muted">
                  No logins. Public images need none — add one when a registry refuses an anonymous
                  pull.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>

      {rotating !== null && (
        <Rotate
          path={`${path}/${rotating}`}
          onDone={() => {
            setRotating(null);
            reload();
          }}
          onError={setError}
        />
      )}

      <Add path={path} onCreated={reload} onError={setError} />
    </>
  );
}

function Rotate({
  path,
  onDone,
  onError,
}: {
  path: string;
  onDone: () => void;
  onError: (m: string) => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  return (
    <Card className="border-brand/50">
      <p className="mb-3 text-xs text-muted">
        The registry stays the same. To point at a different one, delete this and add another —
        changing it in place would silently send an app&apos;s pulls somewhere else.
      </p>
      <form
        className="flex items-end gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.put(path, { username, password });
            onDone();
          } catch (err) {
            onError(message(err));
          }
        }}
      >
        <div className="flex-1">
          <Field label="Username">
            <input className={inputClass} value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
        </div>
        <div className="flex-1">
          <Field label="Password or token">
            <input
              className={inputClass}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </div>
        <Button type="submit" className="mb-3">
          Replace
        </Button>
      </form>
    </Card>
  );
}

function Add({
  path,
  onCreated,
  onError,
}: {
  path: string;
  onCreated: () => void;
  onError: (m: string) => void;
}) {
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  return (
    <Card>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.post(path, { name, host, username, password });
            setName("");
            setHost("");
            setUsername("");
            setPassword("");
            onCreated();
          } catch (err) {
            onError(message(err));
          }
        }}
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name">
            <input
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="DigitalOcean"
            />
          </Field>
          <Field label="Registry" hint="docker.io for the Hub.">
            <input
              className={inputClass}
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="registry.digitalocean.com"
            />
          </Field>
          <Field label="Username">
            <input className={inputClass} value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label="Password or token" hint="An access token wherever the registry offers one.">
            <input
              className={inputClass}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </div>
        <Button type="submit">Add registry</Button>
      </form>
    </Card>
  );
}
