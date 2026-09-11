"use client";

import { cn } from "cn";

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
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ValueCard } from "@/components/value-card";
import { type ApiKey, api, avatarSrc } from "@/lib/api";
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

// Who you are on this instance, and the four things you can say about
// it.
//
// **The username is here and nowhere else in this product.** Every
// other identifier is permanent, because renaming one silently moves
// things other people configured against it — a project's slug is a
// path component of every app's registry reference underneath it. A
// username is written against this account's own rows, and sessions and
// keys are held by id, so both survive. The one thing it breaks belongs
// to whoever is doing it, which is why the field says so rather than
// the field not existing.
function General() {
  const me = useSession();
  const [displayName, setDisplayName] = useState(me.display_name ?? "");
  const [username, setUsername] = useState(me.username);
  const [email, setEmail] = useState(me.email ?? "");
  const [avatar, setAvatar] = useState(me.avatar ?? "");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const dirty =
    displayName !== (me.display_name ?? "") ||
    username !== me.username ||
    email !== (me.email ?? "") ||
    avatar !== (me.avatar ?? "");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      await api.patch("/users/me", {
        display_name: displayName,
        username,
        email,
        avatar,
      });
      // The shell resolved this account once and hands it down; a name
      // or a face changed here has to reach the sidebar, and reloading
      // is the honest way to say "everything that read it reads it
      // again" without a second source of truth for who you are.
      window.location.reload();
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <>
      <SectionHeader title="You" />
      <Card>
        <CardContent>
          <ErrorAlert error={error} />
          <form onSubmit={save} className="space-y-4">
            <TextField
              label="Display name"
              hint="What you are called, which a username often is not. Empty is fine — your username stands in."
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder={me.username}
            />
            <TextField
              label="Username"
              hint="Lowercase letters, digits, dot, dash or underscore. Sessions and API keys survive a change; `docker login` does not — it sends this alongside the key, so log in again after."
              value={username}
              spellCheck={false}
              onChange={(e) => setUsername(e.target.value)}
              className="font-mono"
            />
            <TextField
              label="Email"
              type="email"
              hint="Nothing here sends mail. It is so whoever runs this box can tell whose account is whose, and it is never a second way to sign in."
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />

            <Faces chosen={avatar} onChoose={setAvatar} offered={me.avatars ?? []} />

            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty}>
                Save
              </ActionButton>
              {saved && !dirty && <span className="text-xs text-muted-foreground">Saved.</span>}
            </div>
          </form>
        </CardContent>
      </Card>
    </>
  );
}

// The faces this instance ships, offered as themselves.
//
// A grid of the actual images rather than a select of their names: what
// somebody is choosing is a picture, and a dropdown reading "blue"
// makes them pick one to find out what it looks like. The same argument
// the theme swatches make one tab over.
//
// **"None" is one of the choices**, not a separate clear button. It is
// a state the account can be in — most are — and a control that can
// reach every state but the default is one that needs a second control
// beside it.
function Faces({
  chosen,
  onChoose,
  offered,
}: {
  chosen: string;
  onChoose: (name: string) => void;
  offered: string[];
}) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">Icon</Label>
      <div className="flex flex-wrap gap-3">
        <button
          type="button"
          onClick={() => onChoose("")}
          aria-pressed={chosen === ""}
          className={cn(
            "flex size-12 shrink-0 items-center justify-center border text-[10px] tracking-[0.14em] uppercase transition-colors",
            chosen === ""
              ? "border-primary text-primary"
              : "border-border text-subtle-foreground hover:border-border-strong",
          )}
        >
          None
        </button>
        {offered.map((name) => (
          <button
            key={name}
            type="button"
            onClick={() => onChoose(name)}
            aria-label={name}
            aria-pressed={chosen === name}
            className={cn(
              "size-12 shrink-0 overflow-hidden border transition-colors",
              chosen === name ? "border-primary" : "border-border hover:border-border-strong",
            )}
          >
            {/* A plain <img>: next/image wants a loader and a build-time
                size for something that is four fixed files in this
                image's own public directory. */}
            {/* biome-ignore lint/performance/noImgElement: four static files, no loader worth configuring */}
            <img src={avatarSrc(name)} alt="" className="size-full object-cover" />
          </button>
        ))}
      </div>
    </div>
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
