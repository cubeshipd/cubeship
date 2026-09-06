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
  redis: "In-memory, for caches, queues and sessions.",
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
  project,
  environment,
  open,
  onOpenChange,
  onCreated,
}: {
  project: string;
  environment: string;
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
    setUsername("cubeship");
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
        }
      })
      .catch((e) => setError(message(e)));
  }, [open]);

  const chosen = engines?.find((e) => e.engine === engine);

  function pickEngine(next: string) {
    setEngine(next);
    setVersion(engines?.find((e) => e.engine === next)?.default_version ?? "");
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<CreatedDatastore>("/datastores", {
        name,
        description,
        project,
        environment,
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
              It runs in <code className="text-foreground">{environment}</code>, beside the apps
              there. Attach an app to it afterwards and that app gets the connection string.
            </DialogDescription>
          </DialogHeader>

          <div className="max-h-[65vh] space-y-4 overflow-y-auto py-5">
            <ErrorAlert error={error} className="mb-0" />

            <SlugField label="Name" value={name} onChange={setName} autoFocus />

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

            <TextField
              label="Username"
              value={username}
              spellCheck={false}
              onChange={(e) => setUsername(e.target.value)}
              hint={
                engine === "postgres"
                  ? "The login the server is created with."
                  : "The login the server is created with. Not “root” — MySQL and MariaDB will not create it."
              }
            />

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
