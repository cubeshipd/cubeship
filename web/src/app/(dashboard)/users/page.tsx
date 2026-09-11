"use client";

import {
  KeyRoundIcon,
  LockIcon,
  PlusIcon,
  RotateCcwKeyIcon,
  ShieldIcon,
  Trash2Icon,
  UnlockIcon,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { RowActions, RowMenu, RowMenuItem } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ValueCard } from "@/components/value-card";
import { api, avatarSrc, type InstanceUser, personName } from "@/lib/api";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

const ROLES = [
  { value: "member", label: "Member" },
  { value: "admin", label: "Admin" },
];

// Who can reach this instance at all.
//
// **In Platform rather than under your own settings**, because it is
// about the instance and not about you: who holds a way in is the same
// kind of fact as which registry it pulls from and which machines it is
// made of. Your own password and the colours you see are the other
// thing, and they are under your name.
//
// An **admin's screen including the reading**. The list says who can get
// in, which is not something a member needs and is exactly what somebody
// probing would want — so a member is sent away rather than shown an
// empty table.
//
// **The screen is the table.** Adding somebody used to be a card above
// it, on the grounds that it is what brings anybody here — which was
// true and was still the wrong shape: it is one act, it belongs in the
// rail with every other screen's one act, and a form taking up the top
// third pushed the thing the screen is named after below the fold.
export default function UsersPage() {
  const me = useSession();
  const [users, setUsers] = useState<InstanceUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<InstanceUser | null>(null);
  const [revoking, setRevoking] = useState<InstanceUser | null>(null);
  const [blocking, setBlocking] = useState<InstanceUser | null>(null);
  const [resetting, setResetting] = useState<InstanceUser | null>(null);
  const [changing, setChanging] = useState<InstanceUser | null>(null);
  // A password this instance will never say again — whether it came
  // from creating an account or from resetting one.
  const [issued, setIssued] = useState<{ username: string; password: string } | null>(null);

  useOpenOnArrival("new", setAdding);

  const reload = useCallback(() => {
    api
      .get<{ users: InstanceUser[] }>("/users")
      .then((r) => setUsers(r.users))
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  if (me.role !== "admin") {
    return <ErrorAlert error="Who can reach this instance is an admin's to see." />;
  }

  async function unblock(u: InstanceUser) {
    try {
      await api.patch(`/users/${u.username}`, { blocked: false });
      reload();
    } catch (e) {
      setError(message(e));
    }
  }

  const columns: Column<InstanceUser>[] = [
    {
      id: "name",
      header: "User",
      width: 28,
      sortBy: (u) => personName(u),
      cell: (u) => (
        <span className="flex min-w-0 items-center gap-2.5">
          {/* biome-ignore lint/performance/noImgElement: a static file in this image's own public directory */}
          <img src={avatarSrc(u.avatar, "small")} alt="" className={cnFace(u)} />
          <span className="truncate">{personName(u)}</span>
          {u.username === me.username && (
            <span className="shrink-0 text-subtle-foreground">you</span>
          )}
        </span>
      ),
    },
    {
      // **The one screen where the address is a column of its own.**
      // Everywhere else a person is called what they are called; here
      // the username is the thing you act on — it is the path segment,
      // what `docker login` sends, and what the confirmations below ask
      // you to type.
      id: "username",
      header: "Username",
      width: 20,
      sortBy: (u) => u.username,
      cell: (u) => <span className="truncate font-mono text-xs">{u.username}</span>,
    },
    {
      id: "role",
      header: "Role",
      width: 14,
      sortBy: (u) => u.role,
      cell: (u) => <span className="text-muted-foreground">{u.role}</span>,
    },
    {
      id: "status",
      header: "Status",
      width: 14,
      sortBy: (u) => (u.blocked_at ? "blocked" : "active"),
      // Not a StatusBadge: that colours what a container is doing, and
      // painting a person green for existing would say a great deal
      // less than the one row it needs to pick out.
      //
      // **"Active" rather than a dash.** An empty cell is a fact this
      // screen did not have, and what it means here is the opposite: a
      // column that is blank for everybody who is fine makes the word
      // "Blocked" read as the only value the column ever takes.
      cell: (u) =>
        u.blocked_at ? (
          <span className="text-destructive text-xs uppercase tracking-wide">Blocked</span>
        ) : (
          <span className="text-muted-foreground text-xs uppercase tracking-wide">Active</span>
        ),
    },
    {
      id: "since",
      header: "Since",
      width: 14,
      sortBy: (u) => u.created_at,
      cell: (u) => (
        <span className="text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (u) => {
        // Every refusal the daemon makes, said before the click rather
        // than after it. All three come down to the same thing: an
        // admin must not be able to take their own way in, and the
        // instance must not be left with nobody who can configure it.
        const isYou = u.username === me.username;
        const mine = "This is the account you are signed in as.";
        return (
          <RowActions>
            <RowMenu label={`Actions for ${personName(u)}`}>
              <RowMenuItem
                icon={ShieldIcon}
                disabled={isYou}
                title={isYou ? "You cannot change your own role." : undefined}
                onClick={() => setChanging(u)}
              >
                Change role
              </RowMenuItem>
              <RowMenuItem icon={RotateCcwKeyIcon} onClick={() => setResetting(u)}>
                Reset password
              </RowMenuItem>
              {/* **Blocking asks and unblocking does not.** One takes
                  somebody's way in and the other gives it back, and a
                  confirmation in front of the harmless direction is one
                  people learn to click through on the other. */}
              <RowMenuItem
                icon={u.blocked_at ? UnlockIcon : LockIcon}
                disabled={isYou}
                title={isYou ? mine : undefined}
                onClick={() => (u.blocked_at ? unblock(u) : setBlocking(u))}
              >
                {u.blocked_at ? "Unblock" : "Block"}
              </RowMenuItem>
              <RowMenuItem icon={KeyRoundIcon} onClick={() => setRevoking(u)}>
                Revoke credentials
              </RowMenuItem>
              <RowMenuItem
                icon={Trash2Icon}
                danger
                disabled={isYou}
                title={isYou ? mine : undefined}
                onClick={() => setRemoving(u)}
              >
                Delete account
              </RowMenuItem>
            </RowMenu>
          </RowActions>
        );
      },
    },
  ];

  return (
    <>
      <RailPortal>
        <Button onClick={() => setAdding(true)}>
          <PlusIcon />
          Add user
        </Button>
      </RailPortal>

      <ErrorAlert error={error} />

      {/* Above the table, because it cannot be asked for again and the
          next click must not be able to lose it. */}
      {issued && (
        <ValueCard
          className="mb-4 ring-primary/40"
          label={`${issued.username}'s password — copy it now, it is not shown again`}
          value={issued.password}
        />
      )}

      <DataTable
        columns={columns}
        rows={users}
        rowKey={(u) => u.username}
        search={{
          placeholder: "Filter users",
          by: (u) => [u.display_name, u.username, u.role],
        }}
        empty="Nobody but you."
      />

      <NewUserDialog
        open={adding}
        onOpenChange={setAdding}
        onCreated={(created) => {
          setIssued(created);
          reload();
        }}
      />

      <RoleDialog
        user={changing}
        onOpenChange={(open) => !open && setChanging(null)}
        onSaved={reload}
      />

      <ConfirmDialog
        open={blocking !== null}
        onOpenChange={(open) => !open && setBlocking(null)}
        title={`Block ${blocking ? personName(blocking) : ""}?`}
        confirmLabel="Block"
        description="Every way in is refused from the next request — their password, their keys and the sessions they are signed in on. Nothing is revoked: unblocking puts them back exactly where they were, with what they already had. Anything they deployed keeps running."
        onConfirm={async () => {
          await api.patch(`/users/${blocking?.username}`, { blocked: true });
          setBlocking(null);
          reload();
        }}
      />

      <ConfirmDialog
        open={resetting !== null}
        onOpenChange={(open) => !open && setResetting(null)}
        title={`Issue a new password for ${resetting ? personName(resetting) : ""}?`}
        confirmLabel="Issue password"
        description="You get it once, to hand over — this instance keeps only its hash, and nothing here sends mail. Their API keys are untouched: a forgotten password is not a lost laptop. Every session they hold ends, because the password changed."
        onConfirm={async () => {
          const out = await api.post<{ username: string; password: string }>(
            `/users/${resetting?.username}/password`,
          );
          setIssued(out);
          setResetting(null);
          reload();
        }}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Delete account"
        description="The account goes and its keys and sessions go with it, in one transaction, so nothing that authenticates outlives it. Anything they deployed keeps running. To shut somebody out without losing the account, block it instead."
        confirmWord={removing?.username ?? ""}
        confirmLabel="Delete account"
        onConfirm={async () => {
          await api.del(`/users/${removing?.username}`);
          setRemoving(null);
          reload();
        }}
      />

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title="Revoke credentials"
        description="Every session ends and every API key is revoked, everywhere at once — the answer to a laptop that walked off. The account stays, and so does its password: that is a secret in somebody's head rather than a credential lying on the machine that was lost."
        confirmWord={revoking?.username ?? ""}
        confirmLabel="Revoke everything"
        onConfirm={async () => {
          await api.del(`/users/${revoking?.username}/credentials`);
          setRevoking(null);
          reload();
        }}
      />
    </>
  );
}

// A blocked account's face is dimmed, because the row is otherwise the
// same row and the word in the status column is small.
function cnFace(u: InstanceUser) {
  return u.blocked_at
    ? "size-6 shrink-0 border border-border object-cover opacity-40 grayscale"
    : "size-6 shrink-0 border border-primary/40 object-cover";
}

// Adding an account.
//
// **It hands back a password and no API key.** A key is what a CLI or
// an MCP client carries and the dashboard wants a session — an account
// given a key and no password could not sign in anywhere it had been
// told the address of, which is what this did for a release. Keys are
// self-service, made from their own account screen.
function NewUserDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (created: { username: string; password: string }) => void;
}) {
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("member");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setUsername("");
    setRole("member");
    setError(null);
  }, [open]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<{ username: string; password: string }>("/users", {
        username,
        role,
      });
      onCreated(created);
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
            <DialogTitle>Add user</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Username"
              value={username}
              autoFocus
              spellCheck={false}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="ana"
              hint="Lowercase letters, digits, dot, dash or underscore. It is permanent: it names them everywhere, and it is what they log Docker in as."
            />
            <SearchableSelect
              label="Role"
              searchable={false}
              choices={ROLES}
              value={role}
              onChange={setRole}
              hint="An admin also builds source on this host and configures the instance."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!username.trim()}>
              Add account
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// Moving somebody between the two roles.
//
// A dialog rather than a select in the row: it is two words and a
// consequence, and a control that changes what somebody may do the
// moment it is released is one a stray click operates.
function RoleDialog({
  user,
  onOpenChange,
  onSaved,
}: {
  user: InstanceUser | null;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [role, setRole] = useState("member");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!user) return;
    setRole(user.role);
    setError(null);
  }, [user]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!user) return;
    setBusy(true);
    setError(null);
    try {
      await api.patch(`/users/${user.username}`, { role });
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={user !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{user ? personName(user) : ""}&rsquo;s role</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <SearchableSelect
              label="Role"
              searchable={false}
              choices={ROLES}
              value={role}
              onChange={setRole}
              hint="A member deploys images somebody already published. An admin also builds source on this host — which runs whatever the repository contains, here — and configures the instance."
            />
            <p className="text-[11px] text-muted-foreground">
              Their sessions and keys are untouched and start being refused for what the new role
              does not reach, on their next request.
            </p>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={role === user?.role}>
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
