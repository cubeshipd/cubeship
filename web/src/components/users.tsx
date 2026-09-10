"use client";

import { KeyRoundIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { useSession } from "@/components/session-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ValueCard } from "@/components/value-card";
import { api, type InstanceUser } from "@/lib/api";
import { message } from "@/lib/errors";

// Users is who can reach this instance at all.
//
// **An admin's screen**, reads included: the list says who holds a way
// in, which is not something a member needs and is exactly what
// somebody probing would want. The tab is not offered to one.
export function Users() {
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

  const admins = (users ?? []).filter((u) => u.role === "admin").length;

  return (
    <>
      <SectionHeader
        title="Who has access"
        sub="An account holds a role and the credentials it signs in with. A member deploys images somebody already published; an admin also builds source on this host and configures the instance."
      />
      <ErrorAlert error={error} />

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Since</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(users ?? []).map((u) => {
                const isYou = u.username === me.username;
                // The two refusals the daemon makes, said before the
                // click rather than after it: the account you are
                // signed in as, and the last admin — setup closed when
                // the first account appeared, and nothing in the API
                // can make an admin without one.
                const lastAdmin = u.role === "admin" && admins <= 1;
                return (
                  <TableRow key={u.username}>
                    <TableCell className="font-mono text-xs">
                      {u.username}
                      {isYou && <span className="ml-2 text-subtle-foreground">you</span>}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">{u.role}</TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {new Date(u.created_at).toLocaleDateString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <RowActions>
                        <RowAction
                          icon={KeyRoundIcon}
                          label="Revoke credentials"
                          title={
                            isYou
                              ? "This would sign you out everywhere and revoke your own keys."
                              : undefined
                          }
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
                    </TableCell>
                  </TableRow>
                );
              })}
              {users?.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="text-muted-foreground text-xs">
                    Nobody but you.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Invite onCreated={reload} onError={setError} />

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
  const [issued, setIssued] = useState<{ username: string; key: string } | null>(null);
  const [busy, setBusy] = useState(false);

  return (
    <>
      <SectionHeader
        title="Add someone"
        sub="They get an API key immediately and a password when they set one. The key is shown once — this instance keeps only its hash, the same as every other credential here."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              onError(null);
              try {
                const created = await api.post<{ username: string; api_key: string }>("/users", {
                  username,
                  role,
                });
                setIssued({ username: created.username, key: created.api_key });
                setUsername("");
                onCreated();
              } catch (err) {
                onError(message(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
              <TextField
                label="Username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="ana"
                hint="Lowercase letters, numbers and dashes. It is permanent: it names them everywhere."
              />
              <div className="space-y-1.5">
                {/* A select, not cards: it is a choice between two
                    named things, and the difference is a word. */}
                <label
                  htmlFor="role"
                  className="block font-medium text-[11px] uppercase tracking-wide"
                >
                  Role
                </label>
                <Select value={role} onValueChange={(v) => setRole(String(v))}>
                  <SelectTrigger id="role" className="w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">Member</SelectItem>
                    <SelectItem value="admin">Admin</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <Button type="submit" disabled={busy || username === ""}>
              {busy ? "Adding..." : "Add account"}
            </Button>
          </form>

          {issued && (
            <div className="pt-4">
              <ValueCard
                className="ring-primary/40"
                label={`${issued.username}'s API key — copy it now, it is not shown again`}
                value={issued.key}
              />
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
