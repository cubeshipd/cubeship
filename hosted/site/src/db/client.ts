import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { databaseUrl } from "@/lib/env";
import * as schema from "./schema";

type Handle = ReturnType<typeof drizzle<typeof schema>>;

// Kept on globalThis rather than in a module variable: `next dev` re-evaluates
// modules on every change, and each evaluation would open a pool of its own
// while the old one kept its connections.
const shared = globalThis as typeof globalThis & { cubeshipDb?: Handle };

// Lazy, so importing this module never opens a connection and never
// reads an environment variable.
export function db() {
  if (!shared.cubeshipDb) {
    const pool = new Pool({
      connectionString: databaseUrl(),
      max: 10,
      // Not the OS's ~75s TCP timeout: an unreachable database fails a request in
      // seconds. A connection to a remote database is a TCP handshake plus several
      // round trips of authentication, so the limit leaves room for that; the
      // landing page and sitemap add their own tighter deadline.
      connectionTimeoutMillis: 5_000,
      // A connection costs those round trips again, so an idle one is kept for
      // minutes rather than pg's 10 seconds.
      idleTimeoutMillis: 5 * 60_000,
      keepAlive: true,
      // The OS waits two hours before its first keepalive probe, far longer than a
      // home router keeps an idle connection; a dropped one surfaces as
      // "Connection terminated unexpectedly" on the next query.
      keepAliveInitialDelayMillis: 10_000,
      // A database that accepts the connection but never answers must
      // not hang the landing page or the sitemap either.
      statement_timeout: 5_000,
    });
    // pg emits 'error' on the pool when an idle client's connection dies
    // in the background; with no listener that is an unhandled event,
    // and Node crashes the process for it.
    pool.on("error", (error) => {
      console.error("database pool error:", error.message);
    });
    shared.cubeshipDb = drizzle(pool, { schema });
  }
  return shared.cubeshipDb;
}
