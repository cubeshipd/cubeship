"use client";

import { useCallback, useEffect, useState } from "react";
import { api, type ApiKey } from "@/lib/api";
import { Button, Card, ErrorNote, Field, PageHeader, Shell, inputClass, message } from "@/components/ui";

export default function Account() {
  return (
    <Shell>
      <PageHeader title="Account" sub="Your password, and the API keys that authenticate the CLI and docker." />
      <Keys />
      <Password />
    </Shell>
  );
}

function Keys() {
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [name, setName] = useState("");
  const [issued, setIssued] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api.get<ApiKey[]>("/users/me/api-keys").then(setKeys).catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  return (
    <>
      <h2 className="mb-2.5 mt-7 text-[15px] font-medium">API keys</h2>
      <ErrorNote error={error} />

      {issued && (
        <Card className="border-brand/50">
          <div className="text-xs text-muted">Copy this now — it is not shown again.</div>
          <div className="mt-1.5 font-mono text-sm break-all">{issued}</div>
        </Card>
      )}

      <Card className="p-0">
        <table className="w-full text-sm">
          <tbody>
            {keys?.map((k) => (
              <tr key={k.id} className="border-b border-line last:border-0">
                <td className="p-3">
                  {k.name}
                  {k.current_key && <span className="ml-2 text-xs text-muted">this session</span>}
                </td>
                <td className="p-3 text-xs text-muted">
                  {k.last_used_at ? `last used ${new Date(k.last_used_at).toLocaleString()}` : "never used"}
                </td>
                <td className="p-3 text-right">
                  <button
                    className="text-xs text-muted hover:text-bad"
                    onClick={async () => {
                      try {
                        await api.del(`/users/me/api-keys/${k.id}`);
                        reload();
                      } catch (e) {
                        setError(message(e));
                      }
                    }}
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
            {keys?.length === 0 && (
              <tr>
                <td className="p-3 text-sm text-muted">
                  No keys. You need one for <span className="font-mono text-xs">cubeship login</span>{" "}
                  and <span className="font-mono text-xs">docker login</span>.
                </td>
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
            const created = await api.post<{ api_key: string }>("/users/me/api-keys", { name });
            setIssued(created.api_key);
            setName("");
            reload();
          } catch (err) {
            setError(message(err));
          }
        }}
      >
        <div className="flex-1">
          <Field label="New key">
            <input
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="laptop"
            />
          </Field>
        </div>
        <Button type="submit" className="mb-3">
          Create
        </Button>
      </form>
    </>
  );
}

function Password() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <>
      <h2 className="mb-2.5 mt-7 text-[15px] font-medium">Password</h2>
      <Card>
        <ErrorNote error={error} />
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setError(null);
            setDone(false);
            try {
              await api.put("/users/me/password", { current_password: current, new_password: next });
              setCurrent("");
              setNext("");
              setDone(true);
            } catch (err) {
              setError(message(err));
            }
          }}
        >
          <Field label="Current password">
            <input
              className={inputClass}
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
            />
          </Field>
          <Field label="New password" hint="At least 12 characters. Every other session is signed out.">
            <input
              className={inputClass}
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              autoComplete="new-password"
            />
          </Field>
          <div className="flex items-center gap-3">
            <Button type="submit">Change password</Button>
            {done && <span className="text-xs text-muted">Changed.</span>}
          </div>
        </form>
      </Card>
    </>
  );
}
