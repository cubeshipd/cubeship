import Link from "next/link";
import { StatusBadge } from "@/components/status-badge";
import { type Datastore, datastoreLabel } from "@/lib/api";

// One database in an environment's grid, beside the apps that use it.
//
// The attached apps are on the card because that is what the thing is
// for: a database nothing is attached to is one no app on this instance
// can reach, and that is worth seeing without opening it.
export function DatastoreCard({ datastore }: { datastore: Datastore }) {
  const attached = datastore.attachments.map((a) => a.app);
  return (
    <Link
      href={`/projects/${datastore.project}/${datastore.environment}/databases/${datastore.name}`}
      className="hud-frame group flex flex-col border border-border bg-card transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <div className="flex-1 p-4">
        <div className="flex items-start justify-between gap-3">
          <h3 className="min-w-0 truncate font-mono text-sm text-foreground group-hover:text-primary">
            {datastore.name}
          </h3>
          <StatusBadge value={datastore.status} />
        </div>
        <p className="mt-2 truncate font-mono text-[11px] text-muted-foreground">
          {attached.length > 0 ? attached.join(", ") : "no app attached"}
        </p>
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-2.5">
        <span className="font-mono text-[10px] tracking-[0.16em] text-subtle-foreground uppercase">
          {datastoreLabel(datastore.engine)} {datastore.version}
        </span>
        {/* Exposed is the one fact about a database worth carrying on a
            card you are only glancing at: it is the difference between
            something on a private network and something on the
            internet. */}
        {datastore.exposed_port ? (
          <span className="font-mono text-[10px] tracking-[0.16em] text-warning uppercase">
            exposed
          </span>
        ) : null}
      </div>
    </Link>
  );
}
