import { describe, expect, it } from "vitest";
import type { NormalizedManifest } from "@/lib/manifest";
import { engineOf, sourceOf } from "./preview";

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

describe("the preview's details", () => {
  it("names an app by its image and tag", () => {
    expect(sourceOf(manifest.apps[0])).toBe("nginx:1");
  });

  it("names a database by its engine and version", () => {
    expect(engineOf(manifest.databases[0])).toBe("Postgres 18");
  });
});
