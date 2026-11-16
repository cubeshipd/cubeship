"use client";

import { EyeIcon, EyeOffIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ApiError, api, type Datastore, type DatastoreCredentials, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

// Everything needed to connect, as fields you can copy and cannot edit.
//
// Read-only inputs rather than a table of values: a connection string
// is long, and a field scrolls inside itself and takes one click to
// select, where a wrapped line of prose gives you three lines and a
// chance to miss one. None of it is editable — see Service.Update on
// the daemon for why almost none of it can be.
export function Connection({ datastore }: { datastore: Datastore }) {
  const [creds, setCreds] = useState<DatastoreCredentials | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setCreds(null);
    setForbidden(false);
    api
      .get<DatastoreCredentials>(`${datastorePath(datastore.name)}/credentials`)
      .then(setCreds)
      .catch((e) => {
        // Reading the login is an admin's. A member still gets the
        // address and the user name, which is most of what is on this
        // screen — so the refusal is a note, not an error.
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(message(e));
      });
  }, [datastore.name]);

  return (
    <Card className="mb-4">
      <CardContent className="space-y-5">
        <header className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <h2 className="text-lg font-medium tracking-tight">Connection settings</h2>
            <p className="max-w-2xl text-sm text-muted-foreground">
              Apps on this instance reach it by container name on the shared network.
            </p>
          </div>
          {creds && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => setRevealed(!revealed)}
              aria-label={
                revealed ? "Hide the password and the URIs" : "Reveal the password and the URIs"
              }
            >
              {revealed ? <EyeOffIcon /> : <EyeIcon />}
              {revealed ? "Hide" : "Reveal"}
            </Button>
          )}
        </header>

        <ErrorAlert error={error} />
        {forbidden && (
          <Notice>The password and connection strings are available to administrators only.</Notice>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <CopyField label="Username" value={datastore.username} />
          {datastore.database && <CopyField label="Database" value={datastore.database} />}

          {creds && (
            <CopyField
              label="Password"
              value={creds.password}
              masked={!revealed}
              hint="Copyable while hidden — nothing has to read it to use it."
            />
          )}

          <CopyField
            label="Internal host"
            value={`${datastore.host}:${datastore.port}`}
            hint="The container's own name on the shared network."
          />

          {creds && (
            <CopyField
              label="Internal URI"
              value={creds.internal_uri}
              // The password is inside it. Hiding one and printing the
              // other on the same card would be a lock on a door with
              // the key taped beside it — and this is the field
              // somebody leaves on screen while they go and paste it.
              masked={!revealed}
              fieldClassName="sm:col-span-2"
              hint={`What an attached app already receives as ${datastore.var_stem}_URL. This is for anything wired by hand.`}
            />
          )}

          {creds?.external_uri && (
            <CopyField
              label="External URI"
              value={creds.external_uri}
              masked={!revealed}
              fieldClassName="sm:col-span-2"
              hint="From off this host, on the port this database is published on. There is no TLS in front of it."
            />
          )}
        </div>
      </CardContent>
    </Card>
  );
}
