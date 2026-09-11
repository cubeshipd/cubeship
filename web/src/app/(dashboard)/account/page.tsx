"use client";

import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { Appearance } from "@/components/appearance";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ValueCard } from "@/components/value-card";
import { type ApiKey, api } from "@/lib/api";
import { message } from "@/lib/errors";

// The screen for everything that is **yours** rather than the
// instance's: how you sign in, and what it looks like to you.
//
// Tabs rather than one long page, and reached from the menu under your
// name rather than from the sidebar. The sidebar is what the instance
// is made of and what you deploy on it; your own password is neither,
// and it sat there as a section of one item.
//
// Who else can reach the instance is *not* here — it is in Platform,
// beside the credentials and the machines, because it is a fact about
// the instance rather than about you. `/settings` stays the instance's
// too.
export default function Account() {
  return (
    <Tabs defaultValue="general">
      <TabsList variant="line">
        <TabsTrigger value="general">General</TabsTrigger>
        <TabsTrigger value="appearance">Appearance</TabsTrigger>
        <TabsTrigger value="security">Security</TabsTrigger>
        <TabsTrigger value="keys">API keys</TabsTrigger>
      </TabsList>

      <TabsContent value="general">
        <General />
      </TabsContent>
      <TabsContent value="appearance">
        <Appearance />
      </TabsContent>
      <TabsContent value="security">
        <Password />
      </TabsContent>
      <TabsContent value="keys">
        <Keys />
      </TabsContent>
    </Tabs>
  );
}

// Who you are on this instance.
//
// **Read-only, and all three of them are facts nothing here can
// change.** A username is the identity every session, key and audit
// line is written against; a role is an admin's to grant, and an admin
// editing their own here would be a lock with the key taped to it. The
// third is not a setting either — it is what says what revoking your
// last key costs, which is the question the keys tab asks.
//
// It is a tab of facts, which is the thing a *settings* screen should
// not be — the slug came off the project and environment screens for
// exactly that reason. The difference is what the screen is for: those
// are for configuring a resource, and a fact filed among its fields
// reads as a field that will not take. This screen is your account, and
// the first thing an account screen answers is which account.
function General() {
  const me = useSession();
  return (
    <>
      <SectionHeader title="You" />
      <ValueCard label="Username" value={me.username} />
      <ValueCard label="Role" value={me.role} />
      <ValueCard
        label="Signing in"
        value={
          me.has_password
            ? "A password, and API keys for the CLI"
            : "API keys only — this account has no password"
        }
      />
    </>
  );
}

function Keys() {
  // Whether this account can sign in without a key is what says how
  // much revoking the last one costs. The shell has already resolved
  // it — nothing renders under it until it has — so this does not ask
  // again.
  const me = useSession();
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [name, setName] = useState("");
  const [issued, setIssued] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<ApiKey[]>("/users/me/api-keys")
      .then(setKeys)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  const last = (keys?.length ?? 0) <= 1;

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

      {/* Revoking the last key is allowed — a leaked key has to be able
          to go now, not after you have made a replacement. What stands
          in front of it is a confirmation that says what it costs, and
          that depends on whether this account has another way in. */}
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={`Revoke ${revoking?.name}?`}
        confirmLabel="Revoke"
        description={
          !last ? (
            <>Anything using this key stops working immediately. Your other keys are untouched.</>
          ) : me.has_password ? (
            <>
              This is your only key. <code>cubeship</code> and <code>docker login</code> stop
              working until you create another — which you can do here, because this account signs
              in with a password.
            </>
          ) : (
            <>
              <strong>This is your only key, and this account has no password.</strong> Revoking it
              leaves nothing to authenticate with: another admin would have to let you back in. Set
              a password below first if you want to keep your way in.
            </>
          )
        }
        onConfirm={async () => {
          if (!revoking) return;
          await api.del(`/users/me/api-keys/${revoking.id}`);
          setRevoking(null);
          reload();
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
