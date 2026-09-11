"use client";

import { cn } from "cn";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { Appearance } from "@/components/appearance";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchBar } from "@/components/search-bar";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
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
  const [avatar, setAvatar] = useState(me.avatar);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const dirty =
    displayName !== (me.display_name ?? "") ||
    username !== me.username ||
    email !== (me.email ?? "") ||
    avatar !== me.avatar;

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
// **There is no "none".** It was the first choice here for a release,
// and it was what almost every account held — so the picker's ordinary
// state was the one that showed no picture, and the sidebar drew two
// letters of a username instead. An account arrives on one of these
// now; see user.DefaultAvatar.
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
                size for a handful of fixed files in this image's own
                public directory. */}
            {/* biome-ignore lint/performance/noImgElement: static files, no loader worth configuring */}
            <img src={avatarSrc(name)} alt="" className="size-full object-cover" />
          </button>
        ))}
      </div>
    </div>
  );
}

// The keys this account authenticates a CLI or an MCP client with.
//
// **The same shape as an app's Environment tab**, and deliberately: a
// header with the one action that brings anybody here, a filter, and a
// table. It was a bare `Table` inside a `Card` with a "New key" form
// stuck underneath, which is the arrangement every other listing here
// stopped using — the row actions were a text button rather than the
// icons every other table ends in, and the form below the table meant
// the thing you came to do was the last thing on the screen.
function Keys() {
  // Whether this account can sign in without a key is what says how
  // much revoking the last one costs. The shell has already resolved
  // it — nothing renders under it until it has — so this does not ask
  // again.
  const me = useSession();
  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [adding, setAdding] = useState(false);
  const [filter, setFilter] = useState("");
  const [issued, setIssued] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<ApiKey[]>("/users/me/api-keys")
      .then(setKeys)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  const rows = useMemo(() => {
    if (keys === null) return null;
    const needle = filter.trim().toLowerCase();
    if (!needle) return keys;
    return keys.filter((k) => k.name.toLowerCase().includes(needle));
  }, [keys, filter]);

  const last = (keys?.length ?? 0) <= 1;

  const columns: Column<ApiKey>[] = [
    {
      id: "name",
      header: "Name",
      width: 36,
      sortBy: (k) => k.name,
      cell: (k) => (
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono text-xs">{k.name}</span>
          {/* Which of these is the one you are holding. It is the row
              where revoking has a consequence you feel immediately, and
              nothing else on the screen could tell you which. */}
          {k.current_key && (
            <span className="shrink-0 text-[11px] text-muted-foreground">this session</span>
          )}
        </span>
      ),
    },
    {
      id: "created",
      header: "Created",
      width: 26,
      sortBy: (k) => k.created_at,
      cell: (k) => (
        <span className="text-xs text-muted-foreground">
          {new Date(k.created_at).toLocaleDateString()}
        </span>
      ),
    },
    {
      id: "used",
      header: "Last used",
      width: 28,
      // A key never used sorts below every key that has been, rather
      // than above them: it is the one you are most likely looking for
      // a reason to revoke.
      sortBy: (k) => k.last_used_at ?? "",
      cell: (k) => (
        <span className="text-xs text-muted-foreground">
          {k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "never"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (k) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Revoke ${k.name}`}
            danger
            onClick={() => setRevoking(k)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        title="API keys"
        sub="What the CLI and an MCP client carry. Docker takes one too, alongside your username, when you push to this instance."
        actions={
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <PlusIcon />
            New key
          </Button>
        }
      />
      <ErrorAlert error={error} />

      {/* Above the filter, not inside the dialog that made it. The
          dialog is closed by the time this matters, and a value shown
          once belongs where it cannot be dismissed by the next click. */}
      {issued && (
        <ValueCard
          className="mb-4 ring-primary/40"
          label="Copy this now — it is not shown again."
          value={issued}
        />
      )}

      <SearchBar
        value={filter}
        onChange={setFilter}
        placeholder="Filter by name"
        className="mb-4"
        trailing={
          keys && rows ? (
            <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
              {rows.length === keys.length ? keys.length : `${rows.length}/${keys.length}`}
            </span>
          ) : undefined
        }
      />

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(k) => String(k.id)}
        loadingRows={3}
        empty={
          filter.trim() ? (
            "Nothing matches."
          ) : (
            <>
              No keys. You need one for <code>cubeship login</code> and <code>docker login</code>.
            </>
          )
        }
        className="mb-4"
      />

      <NewKeyDialog
        open={adding}
        onOpenChange={setAdding}
        onIssued={(key) => {
          setIssued(key);
          reload();
        }}
      />

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
    </>
  );
}

// Issuing one, in the dialog every other "add a row" here opens.
//
// **The key is handed back to the screen rather than shown in here.**
// A dialog is dismissed by clicking anywhere, and this is the one value
// on the instance that cannot be asked for again.
function NewKeyDialog({
  open,
  onOpenChange,
  onIssued,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onIssued: (key: string) => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName("");
    setError(null);
  }, [open]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<{ api_key: string }>("/users/me/api-keys", { name });
      onIssued(created.api_key);
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New API key</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Name"
              value={name}
              autoFocus
              spellCheck={false}
              onChange={(e) => setName(e.target.value)}
              placeholder="laptop"
              hint="For you, so a key you no longer recognise is one you can revoke. Name it after the machine or the tool holding it."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!name.trim()}>
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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
