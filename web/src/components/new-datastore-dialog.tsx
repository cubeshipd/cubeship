"use client";

import { cn } from "cn";
import { RefreshCwIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { CopyButton } from "@/components/copy-button";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { OptionCards } from "@/components/option-cards";
import { SlugField } from "@/components/slug-field";
import { TextAreaField, TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  api,
  type CreatedDatastore,
  type DatastoreEngine,
  datastoreLabel,
  generatePassword,
} from "@/lib/api";
import { message } from "@/lib/errors";

// What each engine is, in the sentence somebody choosing between them
// needs. An engine the daemon offers that is not here still appears —
// by its own name, with nothing invented about it.
const BLURBS: Record<string, string> = {
  postgres: "The default, and what most things mean by “a database”.",
  mysql: "Relational, and what a PHP or WordPress stack usually expects.",
  mariadb: "MySQL’s fork. Clients connect to it as MySQL.",
  redis:
    "In-memory, for caches, queues and sessions. Writes to disk, so a restart is not an empty one.",
  mongodb: "Document-oriented, with no fixed schema.",
};

// Creating a database.
//
// The engines and their versions come from the daemon rather than from
// a list here: a version is permanent once a database runs it, so the
// only list worth offering is the one the daemon will accept.
//
// The password arrives generated. Leaving the field empty would make
// the daemon generate one too, but then nobody sees it until they go
// and ask — and a password field somebody has to fill in is a password
// field somebody fills in badly.
export function NewDatastoreDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (created: CreatedDatastore) => void;
}) {
  const [engines, setEngines] = useState<DatastoreEngine[] | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [engine, setEngine] = useState("");
  const [version, setVersion] = useState("");
  const [username, setUsername] = useState("cubeship");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A fresh password per opening, not per keystroke: regenerating under
  // someone who has already copied it is worse than any convenience.
  useEffect(() => {
    if (!open) return;
    setName("");
    setDescription("");
    setUsername("");
    setPassword(generatePassword());
    setError(null);
    api
      .get<DatastoreEngine[]>("/datastores/engines")
      .then((list) => {
        setEngines(list);
        const first = list[0];
        if (first) {
          setEngine(first.engine);
          setVersion(first.default_version);
          setUsername(first.default_username);
        }
      })
      .catch((e) => setError(message(e)));
  }, [open]);

  const chosen = engines?.find((e) => e.engine === engine);

  function pickEngine(next: string) {
    const picked = engines?.find((e) => e.engine === next);
    setEngine(next);
    setVersion(picked?.default_version ?? "");
    // Each engine names its own default login, and Redis has only one:
    // carrying "cubeship" across from Postgres would be a name it
    // refuses.
    setUsername(picked?.default_username ?? "");
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<CreatedDatastore>("/datastores", {
        name,
        description,
        engine,
        version,
        username,
        password,
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
            <DialogTitle>New database</DialogTitle>
            <DialogDescription>
              It belongs to the instance, not to a project. Attach apps to it afterwards — in any
              project — and each one gets the connection string.
            </DialogDescription>
          </DialogHeader>

          {/* This form is long enough to scroll on a laptop, and a
              scroll container clips both axes — `overflow-y: auto`
              computes `overflow-x` to `auto` too, however wide the
              content is. The 1px borders, the focus rings and the
              neon-edge glow all sit at the content edge, so they were
              being shaved off at the left and right. The padding gives
              them room and the negative margin puts the content back
              where it was. */}
          <div className="-mx-2 max-h-[65vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} className="mb-0" />

            {/* scroll-mt is not decoration. Focusing a field inside a
                scroll container makes the browser scroll it into view,
                and it scrolls to the *input* — which put its own label
                above the top edge, so the first field in this form
                arrived unlabelled. The margin is the room the label
                needs. */}
            <SlugField
              label="Name"
              value={name}
              onChange={setName}
              autoFocus
              className="scroll-mt-12"
            />

            <OptionCards
              label="Engine"
              value={engine}
              onChange={pickEngine}
              options={(engines ?? []).map((e) => ({
                value: e.engine,
                title: datastoreLabel(e.engine),
                body: BLURBS[e.engine] ?? `Version ${e.default_version}.`,
              }))}
            />

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Version</Label>
              <div className="flex flex-wrap gap-2">
                {(chosen?.versions ?? []).map((v) => (
                  <Button
                    key={v}
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-pressed={v === version}
                    onClick={() => setVersion(v)}
                    className={cn(
                      "font-mono",
                      v === version && "neon-edge border-primary/60 bg-primary/8 text-foreground",
                    )}
                  >
                    {v}
                  </Button>
                ))}
              </div>
              <p className="text-xs leading-relaxed text-subtle-foreground">
                Permanent. A data directory written by one major version cannot be read by another,
                so changing this later means a second database and a migration.
              </p>
            </div>

            {/* Hidden for an engine that has one login and will not
                take another. A disabled field showing a value you
                cannot change is a question asked for nothing. */}
            {chosen?.has_user !== false && (
              <TextField
                label="Username"
                value={username}
                spellCheck={false}
                onChange={(e) => setUsername(e.target.value)}
                hint={
                  engine === "mysql" || engine === "mariadb"
                    ? "The login the server is created with. Not “root” — it already exists, and this would not be its password."
                    : "The login the server is created with."
                }
              />
            )}

            <TextField
              label="Password"
              value={password}
              spellCheck={false}
              onChange={(e) => setPassword(e.target.value)}
              action={
                <span className="flex items-center gap-1">
                  <CopyButton value={password} label="Copy the password" />
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    aria-label="Generate another password"
                    onClick={() => setPassword(generatePassword())}
                  >
                    <RefreshCwIcon className="size-3.5" />
                  </Button>
                </span>
              }
              hint="Generated for you. Change it if you already have one, but there is nothing to gain from a shorter one."
            />

            <TextAreaField
              label="Description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              hint="What this database holds. Optional."
            />

            <Notice>
              It will be reachable only from apps on this instance. Publishing it on a host port is
              a separate decision, in its settings.
            </Notice>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!name || !engine || !password}>
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
