import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { validateTemplate } from "./index";

const umami = readFileSync(join(__dirname, "fixtures/umami.yaml"), "utf8");

describe("validateTemplate", () => {
  it("accepts the fixture and normalizes it", () => {
    const { ok, diagnostics, manifest } = validateTemplate(umami);

    expect(diagnostics.filter((d) => d.severity === "error")).toEqual([]);
    expect(ok).toBe(true);
    expect(manifest?.schema_version).toBe(1);
    expect(manifest?.environment).toBe("production");
    expect(manifest?.apps[0].source).toEqual({
      type: "image",
      image: "ghcr.io/umami-software/umami",
      tag: "postgresql-v2",
    });
    expect(manifest?.apps[0].internal_host).toBe("cubeship-umami-production-web");
    expect(manifest?.apps[0].limits).toEqual({ cpu: 1, memory_bytes: 1073741824 });
    expect(manifest?.apps[0].domains[0]).toEqual({ host: "${input.domain}", port: 3000 });
    expect(manifest?.databases[0].name).toBe("umami-db");
  });

  it("sorts environment variables, so two publishes of one file are one document", () => {
    const { manifest } = validateTemplate(umami);

    expect(Object.keys(manifest?.apps[0].env ?? {})).toEqual(["APP_SECRET", "DATABASE_TYPE"]);
  });

  it("stops at a syntax error without running the rest", () => {
    const { ok, diagnostics, manifest } = validateTemplate('project: "x\n');

    expect(ok).toBe(false);
    expect(manifest).toBeUndefined();
    expect(diagnostics.every((d) => d.code === "yaml.syntax")).toBe(true);
  });

  it("keeps warnings and still succeeds", () => {
    const { ok, diagnostics } = validateTemplate(
      "version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n",
    );

    expect(ok).toBe(true);
    expect(diagnostics.some((d) => d.severity === "warning")).toBe(true);
  });

  it("does not normalize when a rule was broken", () => {
    const broken = umami.replace("${input.domain}", "analytics.example.com");

    expect(validateTemplate(broken).manifest).toBeUndefined();
  });
});
