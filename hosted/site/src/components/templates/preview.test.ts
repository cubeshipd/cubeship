import { describe, expect, it } from "vitest";
import type { NormalizedManifest } from "@/lib/manifest";
import { describeApp, describeDatabase } from "./preview";

// What product/template normalizes the fixture in its own tests to.
const manifest: NormalizedManifest = {
  schema_version: 1,
  min_cubeship: null,
  project: "umami",
  environment: "production",
  inputs: [{ key: "domain", type: "domain", label: "Where it answers", required: true }],
  databases: [
    {
      key: "db",
      name: "db",
      engine: "postgres",
      version: "18",
      username: null,
      database: null,
      expose: null,
      limits: null,
      port: 5432,
    },
  ],
  stores: [],
  apps: [
    {
      key: "web",
      name: "web",
      source: { type: "image", image: "nginx", tag: "1" },
      port: 3000,
      health: null,
      // biome-ignore lint/suspicious/noTemplateCurlyInString: a reference in a template file, not JavaScript.
      domains: [{ host: "${input.domain}", port: 3000 }],
      attach: [{ kind: "database", key: "db", bucket: null, prefix: "" }],
      env: {},
      limits: null,
      scale: null,
      spread: false,
      autoscale: null,
      internal_host: "cubeship-umami-production-web",
    },
  ],
};

describe("the preview's sentences", () => {
  it("says what an app is and where it answers", () => {
    expect(describeApp(manifest.apps[0], manifest.inputs)).toBe(
      "nginx:1 on port 3000, at the domain from “Where it answers”, reachable inside as cubeship-umami-production-web:3000",
    );
  });

  it("says what a database is", () => {
    expect(describeDatabase(manifest.databases[0])).toBe("postgres 18, named db, on port 5432");
  });
});
