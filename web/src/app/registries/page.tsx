"use client";

import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { api, type RegistryCredential } from "@/lib/api";
import { message } from "@/lib/errors";

// Logins for registries Cubeship does not run. Cubeship's own registry
// needs none of this — it authenticates each user with their API key.
export default function Registries() {
  return (
    <Shell>
      <PageHeader
        title="Registries"
        sub="Logins for registries Cubeship does not run, held by the selected organization. An app with an external image pulls through whichever of these matches its registry."
      />
      <Body />
    </Shell>
  );
}

function Body() {
  const { org, loaded } = useOrg();

  if (loaded && !org) {
    return (
      <Card>
        <CardContent className="py-2 text-sm text-muted-foreground">
          No organization selected. A login belongs to one — pick or create an organization from the
          switcher at the top of the sidebar.
        </CardContent>
      </Card>
    );
  }

  return org ? <List org={org} /> : null;
}

function List({ org }: { org: string }) {
  const [creds, setCreds] = useState<RegistryCredential[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rotating, setRotating] = useState<number | null>(null);

  const path = `/orgs/${org}/registries`;
  const reload = useCallback(() => {
    api
      .get<RegistryCredential[]>(path)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  return (
    <>
      <ErrorAlert error={error} />

      <Card className="mb-4 py-0">
        <Table>
          <TableBody>
            {creds?.map((c) => (
              <TableRow key={c.id}>
                <TableCell className="px-4 py-2.5">{c.name}</TableCell>
                <TableCell className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                  {c.host}
                </TableCell>
                <TableCell className="px-4 py-2.5 text-xs text-muted-foreground">
                  {c.username}
                </TableCell>
                <TableCell className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    size="xs"
                    className="text-muted-foreground"
                    onClick={() => setRotating(c.id)}
                  >
                    Replace login
                  </Button>
                  <Button
                    variant="ghost"
                    size="xs"
                    className="ml-1 text-muted-foreground hover:text-destructive"
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
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {creds?.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell className="px-4 py-3 text-sm text-muted-foreground">
                  No logins. Public images need none — add one when a registry refuses an anonymous
                  pull.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
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
  const [busy, setBusy] = useState(false);

  return (
    <Card className="mb-4 ring-primary/40">
      <CardContent>
        <p className="mb-4 text-xs text-muted-foreground">
          The registry stays the same. To point at a different one, delete this and add another —
          changing it in place would silently send an app&apos;s pulls somewhere else.
        </p>
        <form
          className="flex items-end gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              await api.put(path, { username, password });
              onDone();
            } catch (err) {
              onError(message(err));
            }
            setBusy(false);
          }}
        >
          <TextField
            label="Username"
            fieldClassName="flex-1"
            className="h-8"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
          <TextField
            label="Password or token"
            fieldClassName="flex-1"
            className="h-8"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <ActionButton type="submit" busy={busy} variant="outline">
            Replace
          </ActionButton>
        </form>
      </CardContent>
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
  const [busy, setBusy] = useState(false);

  return (
    <Card>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
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
            setBusy(false);
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="DigitalOcean"
            />
            <TextField
              label="Registry"
              hint="docker.io for the Hub."
              spellCheck={false}
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="registry.digitalocean.com"
            />
            <TextField
              label="Username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
            <TextField
              label="Password or token"
              hint="An access token wherever the registry offers one."
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <ActionButton type="submit" busy={busy} variant="outline">
            Add registry
          </ActionButton>
        </form>
      </CardContent>
    </Card>
  );
}
