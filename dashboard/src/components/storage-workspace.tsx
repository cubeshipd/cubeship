"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  FileTextIcon,
  HardDriveIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { ErrorAlert } from "@/components/error-alert";
import Link from "@/components/navigation-link";
import { NewObjectStoreDialog } from "@/components/new-object-store-dialog";
import { SearchBar } from "@/components/search-bar";
import { useSession } from "@/components/session-context";
import { MobileNavigation } from "@/components/shell";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, can, objectStorePath } from "@/lib/api";
import { PROVIDER_ICONS } from "@/lib/credentials";
import { storesQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

const CreateContext = createContext<() => void>(() => {});

export const useCreateStorage = () => useContext(CreateContext);

export function StorageWorkspace({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const client = useQueryClient();
  const me = useSession();
  const inventory = useQuery(storesQuery);
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);
  const [stopOpen, setStopOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<{ store: string; message: string } | null>(null);
  const selected = pathname.split("/")[2] ?? "";
  const store = inventory.data?.find((item) => item.name === selected);
  const canManage = can(me, "storage", "manage");
  const stopped = store?.status === "stopped" || store?.status === "failed";
  const canStart = canManage && store?.kind === "managed" && stopped;
  const canStop =
    canManage &&
    store?.kind === "managed" &&
    store.has_container &&
    !stopped &&
    store.status !== "provisioning";
  useOpenOnArrival("new", setCreating);

  const matches = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (inventory.data ?? []).filter((store) =>
      [store.name, store.provider, store.provider_label, store.kind].some((part) =>
        part.toLowerCase().includes(needle),
      ),
    );
  }, [inventory.data, query]);

  useEffect(() => {
    if (pathname !== "/storage" || inventory.isFetching || inventory.isError) return;
    const first = inventory.data?.[0];
    if (first) router.replace(`/storage/${encodeURIComponent(first.name)}`);
  }, [inventory.data, inventory.isError, inventory.isFetching, pathname, router]);

  async function power(action: "start" | "stop") {
    if (!store) return;
    setBusy(true);
    setActionError(null);
    try {
      await api.post(`${objectStorePath(store.name)}/${action}`);
      await client.invalidateQueries({ queryKey: ["objectstores"] });
    } catch (error) {
      setActionError({ store: store.name, message: message(error) });
    } finally {
      setBusy(false);
      setStopOpen(false);
    }
  }

  return (
    <CreateContext.Provider value={() => setCreating(true)}>
      <div className="flex min-h-[calc(100dvh-156px)] min-w-0 flex-col lg:min-h-[calc(100dvh-100px)]">
        <div className="mb-3 flex justify-end lg:hidden">
          <MobileNavigation me={me} />
        </div>
        <div className="grid min-w-0 flex-1 gap-5 lg:grid-cols-[13rem_minmax(0,1fr)]">
          <aside className="min-w-0 lg:sticky lg:top-4 lg:self-start">
            <section className="mb-5">
              <h2 className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Actions
              </h2>
              <div className="grid gap-1">
                {canManage && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    onClick={() => setCreating(true)}
                  >
                    <PlusIcon />
                    Add storage
                  </Button>
                )}
                {(canStart || canStop) && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    disabled={busy}
                    onClick={canStart ? () => void power("start") : () => setStopOpen(true)}
                  >
                    {canStart ? <PlayIcon /> : <PauseIcon />}
                    {busy ? "Working…" : canStart ? "Start" : "Stop"}
                  </Button>
                )}
                {store?.kind === "managed" && store.has_container && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    onClick={() => setLogsOpen(true)}
                  >
                    <FileTextIcon />
                    Logs
                  </Button>
                )}
                {actionError?.store === store?.name && (
                  <p role="alert" className="px-3 text-xs text-destructive">
                    {actionError?.message}
                  </p>
                )}
                {store && canManage && (
                  <Button
                    variant="ghost"
                    className="w-full justify-start text-destructive transition-colors hover:bg-destructive/20 dark:hover:bg-destructive/20 hover:text-destructive"
                    onClick={() => setDeleteOpen(true)}
                  >
                    <Trash2Icon />
                    Delete storage
                  </Button>
                )}
              </div>
            </section>

            <h2 className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Object storage
            </h2>
            <SearchBar
              value={query}
              onChange={setQuery}
              placeholder="Filter storage"
              className="mb-2"
            />
            <nav
              aria-label="Object storage"
              className="flex gap-1 overflow-x-auto rounded-xl border border-border bg-card p-1 lg:flex-col lg:overflow-visible lg:border-0 lg:bg-transparent lg:p-0"
            >
              {inventory.isPending && (
                <p role="status" className="px-3 py-2 text-xs text-muted-foreground">
                  Loading storage…
                </p>
              )}
              {matches.map((store) => {
                const Icon =
                  store.kind === "managed"
                    ? HardDriveIcon
                    : (PROVIDER_ICONS[store.provider] ?? HardDriveIcon);
                const active = selected === store.name;
                return (
                  <Link
                    key={store.name}
                    href={`/storage/${encodeURIComponent(store.name)}`}
                    aria-current={active ? "page" : undefined}
                    className={`flex min-h-12 shrink-0 items-center gap-3 rounded-lg px-3 text-sm transition-colors lg:w-full ${active ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-secondary hover:text-foreground"}`}
                  >
                    <Icon className="size-4 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{store.name}</span>
                      <span className="block truncate text-xs opacity-70">
                        {store.provider_label}
                      </span>
                    </span>
                    <span className="hidden 2xl:block">
                      <StatusBadge value={store.status} />
                    </span>
                  </Link>
                );
              })}
              {!inventory.isPending && matches.length === 0 && (
                <p className="px-3 py-2 text-xs text-muted-foreground">
                  {query ? "No stores match." : "No object storage yet."}
                </p>
              )}
            </nav>
            {inventory.isError && <ErrorAlert error={message(inventory.error)} />}
          </aside>
          <div className="flex min-w-0 flex-col">{children}</div>
        </div>
      </div>
      <NewObjectStoreDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(store) => {
          setCreating(false);
          void client.invalidateQueries({ queryKey: ["objectstores"] });
          router.push(`/storage/${encodeURIComponent(store.name)}`);
        }}
      />
      {store?.kind === "managed" && (
        <>
          <ConfirmDialog
            open={stopOpen}
            onOpenChange={setStopOpen}
            title="Stop this store?"
            confirmLabel="Stop"
            description="Apps that read or write to this store will lose access until it starts again."
            onConfirm={() => power("stop")}
          />
          <ContainerLogsDialog
            open={logsOpen}
            onOpenChange={setLogsOpen}
            path={objectStorePath(store.name)}
            name={store.name}
            hasContainer={store.has_container}
          />
        </>
      )}
      {store && (
        <ConfirmDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title="Delete this store?"
          confirmLabel="Delete storage"
          holdToConfirm
          description={
            store.kind === "managed" ? (
              <>
                Its container and <strong>every object on this host</strong> will be deleted. There
                is no backup.
              </>
            ) : (
              <>
                This instance forgets the endpoint and account.{" "}
                <strong>The bucket and its objects stay untouched.</strong>
              </>
            )
          }
          onConfirm={async () => {
            await api.del(objectStorePath(store.name));
            await client.invalidateQueries({ queryKey: ["objectstores"] });
            setDeleteOpen(false);
            router.replace("/storage");
          }}
        />
      )}
    </CreateContext.Provider>
  );
}
