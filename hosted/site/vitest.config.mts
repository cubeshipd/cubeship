import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// The alias is Next's, repeated: vitest does not read tsconfig paths.
export default defineConfig({
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    // *.db.test.ts round-trips a real Postgres per assertion; 5s is not
    // enough headroom when that database is remote.
    testTimeout: 15_000,
    // withDatabase() truncates shared tables rather than giving each test
    // its own schema, so two files racing against one Postgres corrupt
    // each other's rows mid-transaction. One file at a time keeps it safe.
    fileParallelism: false,
  },
});
