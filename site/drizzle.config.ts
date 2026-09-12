import { defineConfig } from "drizzle-kit";

// Read at module scope on purpose: this file is only ever loaded by the
// drizzle-kit CLI, never by the server.
export default defineConfig({
  schema: "./src/db/schema.ts",
  out: "./drizzle",
  dialect: "postgresql",
  dbCredentials: { url: process.env.DATABASE_URL ?? "" },
});
