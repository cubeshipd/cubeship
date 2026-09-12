import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { databaseUrl } from "@/lib/env";
import * as schema from "./schema";

let handle: ReturnType<typeof drizzle<typeof schema>> | undefined;

// Lazy, so importing this module never opens a connection and never
// reads an environment variable.
export function db() {
  if (!handle) {
    const pool = new Pool({
      connectionString: databaseUrl(),
      max: 10,
      // Not the OS's ~75s TCP timeout: an unreachable database fails a request in
      // seconds. Short enough to fail fast, long enough for a remote database's
      // handshake; the landing page and sitemap add their own tighter deadline.
      connectionTimeoutMillis: 3_000,
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
    handle = drizzle(pool, { schema });
  }
  return handle;
}
