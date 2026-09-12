import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { databaseUrl } from "@/lib/env";
import * as schema from "./schema";

let handle: ReturnType<typeof drizzle<typeof schema>> | undefined;

// Lazy, so importing this module never opens a connection and never
// reads an environment variable.
export function db() {
  if (!handle) {
    handle = drizzle(new Pool({ connectionString: databaseUrl(), max: 10 }), { schema });
  }
  return handle;
}
