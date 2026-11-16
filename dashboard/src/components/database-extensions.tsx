"use client";
import { CheckIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SearchBar } from "@/components/search-bar";
import { SectionHeader } from "@/components/section-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  api,
  type Datastore,
  type DatastoreEngine,
  type DatastoreEngineExtension,
  datastorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// The extensions this database has, and the ones it could have.
//
// Its own tab rather than a block on the create form: the form is
// already long enough to scroll on a laptop, and this is a decision
// somebody can make later — unlike the engine and the version, which
// they cannot.
//
// **Most of them interrupt nothing.** A contrib module is inside every
// Postgres image Cubeship runs, so installing one is a statement against
// the server that is up. Two are not, and only those warn: one needs a
// different image, the other needs its library loaded at startup, and
// both mean the container is replaced.
//
// **Nothing here removes one**, and the missing button is the point:
// removing means running an image without a library that a column's
// type, an index or a default may already need.
//
// A grid of small cards with a filter above it, and no pages. There are
// more than twenty and they are each a line long, so a list would be a
// screen of scrolling to reach the one somebody came for — and paging
// through a set this size is worse than either.
export function Extensions({
  datastore,
  onChanged,
}: {
  datastore: Datastore;
  onChanged: () => void;
}) {
  const [offered, setOffered] = useState<DatastoreEngineExtension[] | null>(null);
  const [query, setQuery] = useState("");
  const [asked, setAsked] = useState<DatastoreEngineExtension | null>(null);
  const [error, setError] = useState<string | null>(null);

  // From the daemon, because which extensions exist at which version is
  // the daemon's list — a copy out here would offer something it then
  // refuses.
  useEffect(() => {
    api
      .get<DatastoreEngine[]>("/datastores/engines")
      .then((list) => {
        const engine = list.find((e) => e.engine === datastore.engine);
        setOffered(
          (engine?.extensions ?? []).filter((x) => x.versions.includes(datastore.version)),
        );
      })
      .catch((e) => setError(message(e)));
  }, [datastore.engine, datastore.version]);

  const needle = query.trim().toLowerCase();
  // Over the name and the sentence beside it, because half of these are
  // known by what they do rather than by what they are called: "fuzzy"
  // should find pg_trgm.
  const matches = (offered ?? []).filter(
    (x) =>
      needle === "" ||
      x.name.includes(needle) ||
      x.sql_name.includes(needle) ||
      x.summary.toLowerCase().includes(needle),
  );
  const installed = matches.filter((x) => datastore.extensions.includes(x.name));
  const available = matches.filter((x) => !datastore.extensions.includes(x.name));
  // While the container is being replaced there is nothing useful to
  // click: a second install would race the first.
  const settling = datastore.status === "provisioning";

  function card(x: DatastoreEngineExtension, on: boolean) {
    return (
      <Card key={x.name} className="flex flex-col">
        <CardContent className="flex flex-1 flex-col gap-2">
          <div className="flex items-start justify-between gap-2">
            <span className="font-mono text-sm break-all text-foreground">{x.name}</span>
            {on ? (
              <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                <CheckIcon className="size-3.5" />
                Installed
              </span>
            ) : (
              <Button
                variant="outline"
                size="xs"
                disabled={settling}
                title={settling ? "This database is being provisioned." : undefined}
                onClick={() => setAsked(x)}
              >
                Install
              </Button>
            )}
          </div>
          <p className="text-xs leading-relaxed text-subtle-foreground">{x.summary}</p>
          {!on && x.requires.length > 0 && (
            <p className="text-xs leading-relaxed text-subtle-foreground">
              Installs {x.requires.join(", ")} with it.
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <SectionHeader
        title="Extensions"
        sub="Install PostgreSQL extensions. Some replace the container briefly; database files stay untouched."
      />

      {offered !== null && offered.length > 0 && (
        <SearchBar
          value={query}
          onChange={setQuery}
          placeholder="Filter extensions"
          className="mb-4"
          count={{ value: matches.length, singular: "extension", plural: "extensions" }}
        />
      )}

      <ErrorAlert error={error} />

      {offered === null && <LoadingList rows={3} />}

      {offered?.length === 0 && (
        <Notice>
          This release offers no extensions for {datastore.engine} {datastore.version}.
        </Notice>
      )}

      {offered !== null && offered.length > 0 && matches.length === 0 && (
        <Notice>Nothing matches “{query}”.</Notice>
      )}

      {installed.length > 0 && (
        <>
          <p className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
            Installed
          </p>
          <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {installed.map((x) => card(x, true))}
          </div>
        </>
      )}

      {available.length > 0 && (
        <>
          <p className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
            Available
          </p>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {available.map((x) => card(x, false))}
          </div>
        </>
      )}

      {/* A deliberate second click and no word to type. It is not
          destructive — the data is not touched — and for most of these
          nothing even pauses, so the dialog says which kind this is
          rather than warning about downtime that is not coming. */}
      <ConfirmDialog
        open={asked !== null}
        onOpenChange={(v) => !v && setAsked(null)}
        title={`Install ${asked?.name ?? ""}?`}
        description={
          asked?.builtin ? (
            <>
              It is already inside this database&apos;s image, so this creates it and nothing else —{" "}
              <code className="text-foreground">{datastore.name}</code> keeps answering throughout.
              It cannot be removed afterwards.
            </>
          ) : (
            <>
              This replaces <code className="text-foreground">{datastore.name}</code>&apos;s
              container, so it stops answering for a few seconds. Its data is a directory on the
              host and is not touched. It cannot be removed afterwards.
            </>
          )
        }
        confirmLabel="Install"
        onConfirm={async () => {
          if (!asked) return;
          await api.post(`${datastorePath(datastore.name)}/extensions`, {
            extensions: [asked.name],
          });
          setAsked(null);
          onChanged();
        }}
      />
    </>
  );
}
