"use client";

import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { ValueCard } from "@/components/value-card";
import { type ApiKey, api } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Account() {
  return (
    <>
      <PageHeader title="Account" />
      <Keys />
      <Password />
    </>
  );
}

function Keys() {
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [name, setName] = useState("");
  const [issued, setIssued] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<ApiKey[]>("/users/me/api-keys")
      .then(setKeys)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<{ api_key: string }>("/users/me/api-keys", { name });
      setIssued(created.api_key);
      setName("");
      reload();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <SectionHeader title="API keys" />
      <ErrorAlert error={error} />

      {issued && (
        <ValueCard
          className="ring-primary/40"
          label="Copy this now — it is not shown again."
          value={issued}
        />
      )}

      <Card className="mb-4 py-0">
        <Table>
          <TableBody>
            {keys?.map((k) => (
              <TableRow key={k.id}>
                <TableCell className="px-4 py-2.5">
                  {k.name}
                  {k.current_key && (
                    <span className="ml-2 text-xs text-muted-foreground">this session</span>
                  )}
                </TableCell>
                <TableCell className="px-4 py-2.5 text-xs text-muted-foreground">
                  {k.last_used_at
                    ? `last used ${new Date(k.last_used_at).toLocaleString()}`
                    : "never used"}
                </TableCell>
                <TableCell className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    size="xs"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => setRevoking(k)}
                  >
                    Revoke
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {keys?.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell className="px-4 py-3 text-sm text-muted-foreground">
                  No keys. You need one for <code>cubeship login</code> and{" "}
                  <code>docker login</code>.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Revoking cannot be undone, and the key in the way is often the
          one this session is holding: whatever is configured with it
          stops authenticating the moment the button lands. */}
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={`Revoke ${revoking?.name}?`}
        confirmWord={revoking?.name}
        confirmLabel="Revoke"
        description={
          revoking?.current_key
            ? "This is the key this session is using. Revoking it signs this session out of the API, and anything else configured with it stops working."
            : "Anything configured with this key stops authenticating immediately. It cannot be brought back — issue a new one instead."
        }
        onConfirm={async () => {
          if (!revoking) return;
          try {
            await api.del(`/users/me/api-keys/${revoking.id}`);
            reload();
          } catch (e) {
            setError(message(e));
          }
          setRevoking(null);
        }}
      />

      <form className="flex items-end gap-2" onSubmit={create}>
        <TextField
          label="New key"
          fieldClassName="flex-1"
          className="h-8"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="laptop"
        />
        <ActionButton type="submit" busy={busy} variant="outline">
          Create
        </ActionButton>
      </form>
    </>
  );
}

function Password() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function change(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
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
    setBusy(false);
  }

  return (
    <>
      <SectionHeader title="Password" />
      <Card>
        <CardContent>
          <ErrorAlert error={error} />
          <form onSubmit={change} className="space-y-4">
            <TextField
              label="Current password"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
            />
            <TextField
              label="New password"
              hint="At least 12 characters. Every other session is signed out."
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              autoComplete="new-password"
            />
            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} variant="outline">
                Change password
              </ActionButton>
              {done && <span className="text-xs text-muted-foreground">Changed.</span>}
            </div>
          </form>
        </CardContent>
      </Card>
    </>
  );
}
