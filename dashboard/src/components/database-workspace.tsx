"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DatabaseIcon,
  FileTextIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { MariaDBIcon, MongoDBIcon, MySQLIcon, PostgreSQLIcon, RedisIcon } from "@/components/icons";
import Link from "@/components/navigation-link";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { SearchBar } from "@/components/search-bar";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { WorkspaceActions } from "@/components/workspace-actions";
import { can, datastoreLabel } from "@/lib/api";
import { datastoresQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

type DatabaseWorkspaceActions = {
  start?: () => void;
  stop?: () => void;
  logs?: () => void;
  delete?: () => void;
  canStart?: boolean;
  canStop?: boolean;
  hasLogs?: boolean;
  busy?: boolean;
  error?: string | null;
};

const Actions = createContext<(value: DatabaseWorkspaceActions) => void>(() => {});
const CreateDatabase = createContext<() => void>(() => {});

export function useDatabaseWorkspaceActions() {
  return useContext(Actions);
}

export function useCreateDatabase() {
  return useContext(CreateDatabase);
}

const ICONS = {
  postgres: PostgreSQLIcon,
  mysql: MySQLIcon,
  mariadb: MariaDBIcon,
  redis: RedisIcon,
  mongodb: MongoDBIcon,
} as const;

export function DatabaseWorkspace({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const client = useQueryClient();
  const me = useSession();
  const inventory = useQuery(datastoresQuery);
  const [actions, setActions] = useState<DatabaseWorkspaceActions>({});
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const selected = pathname.split("/")[2] ?? "";
  const canManage = can(me, "databases", "manage");
  const matches = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (inventory.data ?? []).filter((database) =>
      [database.name, database.engine, database.version].some((part) =>
        part.toLowerCase().includes(needle),
      ),
    );
  }, [inventory.data, query]);

  useEffect(() => {
    if (pathname !== "/databases" || inventory.isFetching || inventory.isError) return;
    const first = inventory.data?.[0];
    if (first) router.replace(`/databases/${first.name}`);
  }, [inventory.data, inventory.isError, inventory.isFetching, pathname, router]);

  return (
    <Actions.Provider value={setActions}>
      <CreateDatabase.Provider value={() => setCreating(true)}>
        <div className="flex min-h-[calc(100dvh-156px)] min-w-0 flex-col lg:min-h-[calc(100dvh-100px)]">
          <div className="grid min-w-0 flex-1 gap-5 lg:grid-cols-[13rem_minmax(0,1fr)]">
            <aside className="min-w-0 lg:sticky lg:top-4 lg:self-start">
              <WorkspaceActions>
                {canManage && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    onClick={() => setCreating(true)}
                  >
                    <PlusIcon />
                    New database
                  </Button>
                )}
                {(actions.canStart || actions.canStop) && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    disabled={actions.busy}
                    onClick={actions.canStart ? actions.start : actions.stop}
                  >
                    {actions.canStart ? <PlayIcon /> : <PauseIcon />}
                    {actions.busy ? "Working…" : actions.canStart ? "Start" : "Stop"}
                  </Button>
                )}
                {actions.hasLogs && (
                  <Button variant="ghost" className="w-full justify-start" onClick={actions.logs}>
                    <FileTextIcon />
                    Logs
                  </Button>
                )}
                {actions.error && (
                  <p role="alert" className="px-3 text-xs text-destructive">
                    {actions.error}
                  </p>
                )}
                {actions.delete && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start text-destructive transition-colors hover:bg-destructive/20 dark:hover:bg-destructive/20 hover:text-destructive"
                    onClick={actions.delete}
                  >
                    <Trash2Icon />
                    Delete database
                  </Button>
                )}
              </WorkspaceActions>

              <h2 className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Databases
              </h2>
              <SearchBar
                value={query}
                onChange={setQuery}
                placeholder="Filter databases"
                className="mb-2"
              />
              <nav
                aria-label="Databases"
                className="flex gap-1 overflow-x-auto rounded-xl border border-border bg-card p-1 lg:flex-col lg:overflow-visible lg:border-0 lg:bg-transparent lg:p-0"
              >
                {inventory.isPending && (
                  <p role="status" className="px-3 py-2 text-xs text-muted-foreground">
                    Loading databases…
                  </p>
                )}
                {matches.map((database) => {
                  const Icon = ICONS[database.engine as keyof typeof ICONS] ?? DatabaseIcon;
                  const active = selected === database.name;
                  return (
                    <Link
                      key={database.name}
                      href={`/databases/${database.name}`}
                      aria-current={active ? "page" : undefined}
                      className={`flex min-h-12 shrink-0 items-center gap-3 rounded-lg px-3 text-sm transition-colors lg:w-full ${active ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-secondary hover:text-foreground"}`}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{database.name}</span>
                        <span className="block truncate text-xs opacity-70">
                          {datastoreLabel(database.engine)} {database.version}
                        </span>
                      </span>
                      <span className="hidden 2xl:block">
                        <StatusBadge value={database.status} />
                      </span>
                    </Link>
                  );
                })}
                {!inventory.isPending && matches.length === 0 && (
                  <p className="px-3 py-2 text-xs text-muted-foreground">
                    {query ? "No databases match." : "No databases yet."}
                  </p>
                )}
              </nav>
              {inventory.isError && <ErrorAlert error={message(inventory.error)} />}
            </aside>

            <div className="flex min-w-0 flex-col">{children}</div>
          </div>
        </div>
        <NewDatastoreDialog
          open={creating}
          onOpenChange={setCreating}
          onCreated={(database) => {
            setCreating(false);
            void client.invalidateQueries({ queryKey: ["datastores"] });
            router.push(`/databases/${database.name}`);
          }}
        />
      </CreateDatabase.Provider>
    </Actions.Provider>
  );
}
