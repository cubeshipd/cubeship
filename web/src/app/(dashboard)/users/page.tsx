"use client";

import { KeyRoundIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ValueCard } from "@/components/value-card";
import { api, type InstanceUser } from "@/lib/api";
import { message } from "@/lib/errors";

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
export default function UsersPage() {
  const me = useSession();
  const [users, setUsers] = useState<InstanceUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<InstanceUser | null>(null);
  const [revoking, setRevoking] = useState<InstanceUser | null>(null);

  const reload = useCallback(() => {
    api
      .get<{ users: InstanceUser[] }>("/users")
      .then((r) => setUsers(r.users))
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  if (me.role !== "admin") {
    return (
      <>
        <ErrorAlert error="Who can reach this instance is an admin's to see." />
      </>
    );
  }

  const admins = (users ?? []).filter((u) => u.role === "admin").length;

  const columns: Column<InstanceUser>[] = [
    {
      id: "username",
      header: "User",
      width: 40,
      sortBy: (u) => u.username,
      cell: (u) => (
        <span className="font-mono">
          {u.username}
          {u.username === me.username && <span className="ml-2 text-subtle-foreground">you</span>}
        </span>
      ),
    },
    {
      id: "role",
      header: "Role",
      width: 20,
      sortBy: (u) => u.role,
      cell: (u) => <span className="text-muted-foreground">{u.role}</span>,
    },
    {
      id: "since",
      header: "Since",
      width: 22,
      sortBy: (u) => u.created_at,
      cell: (u) => (
        <span className="text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 18,
      align: "right",
      cell: (u) => {
        const isYou = u.username === me.username;
        // The two refusals the daemon makes, said before the click
        // rather than after it: the account you are signed in as, and
        // the last admin — setup closed when the first account
        // appeared, and nothing in the API can make an admin without
        // one.
        const lastAdmin = u.role === "admin" && admins <= 1;
        return (
          <RowActions>
            <RowAction
              icon={KeyRoundIcon}
              label="Revoke credentials"
              onClick={() => setRevoking(u)}
            />
            <RowAction
              icon={Trash2Icon}
              label="Delete"
              danger
              disabled={isYou || lastAdmin}
              title={
                isYou
                  ? "You cannot delete the account you are signed in as."
                  : lastAdmin
                    ? "The last admin cannot go: nothing in the API can make another."
                    : undefined
              }
              onClick={() => setRemoving(u)}
            />
          </RowActions>
        );
      },
    },
  ];

  return (
    <>
      <ErrorAlert error={error} />

      {/* Adding somebody comes first, because that is what brings
          anybody to this screen: the table is the answer to "who is
          there", and you already know when it is only you. */}
      <Invite onCreated={reload} onError={setError} />

      <SectionHeader
        title="Who has access"
        sub="An account holds a role and the credentials it signs in with. A member deploys images somebody already published; an admin also builds source on this host and configures the instance."
      />
      <DataTable
        columns={columns}
        rows={users}
        rowKey={(u) => u.username}
        empty="Nobody but you."
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Delete account"
        description="The account goes and its keys and sessions go with it, in one transaction, so nothing that authenticates outlives it. Anything they deployed keeps running."
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

// Invite adds an account.
//
// **It hands back an API key and no password.** An account made here
// gets a way in immediately and sets its own password when it first
// signs in — which is why the key is shown once, here, and never again.
function Invite({
  onCreated,
  onError,
}: {
  onCreated: () => void;
  onError: (m: string | null) => void;
}) {
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("member");
  const [issued, setIssued] = useState<{ username: string; password: string } | null>(null);
  const [busy, setBusy] = useState(false);

  return (
    <>
      <SectionHeader
        title="Add someone"
        sub="They get a password to sign in with, shown once — this instance keeps only its hash, the same as every other credential here. Hand it over and they change it on their own account screen. API keys are theirs to make, from the same place."
      />
      <Card className="mb-6">
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              onError(null);
              try {
                const created = await api.post<{ username: string; password: string }>("/users", {
                  username,
                  role,
                });
                setIssued({ username: created.username, password: created.password });
                setUsername("");
                onCreated();
              } catch (err) {
                onError(message(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <div className="grid gap-4 sm:grid-cols-[1fr_14rem]">
              <TextField
                label="Username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="ana"
                hint="Lowercase letters, numbers and dashes. It is permanent: it names them everywhere."
              />
              {/* The same component every other form's choice uses, so
                  it is the same height as the field beside it — which
                  a bare Select is not. */}
              <SearchableSelect
                label="Role"
                searchable={false}
                choices={[
                  { value: "member", label: "Member" },
                  { value: "admin", label: "Admin" },
                ]}
                value={role}
                onChange={setRole}
                hint="An admin also builds source here and configures the instance."
              />
            </div>
            <Button type="submit" disabled={busy || username === ""}>
              {busy ? "Adding..." : "Add account"}
            </Button>
          </form>

          {issued && (
            <div className="pt-4">
              <ValueCard
                className="ring-primary/40"
                label={`${issued.username}'s password — copy it now, it is not shown again`}
                value={issued.password}
              />
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
