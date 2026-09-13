import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";
import { fromZod, manifestSchema } from "./schema";

const minimal = `version: 1
project: umami
apps:
  - key: web
    image: ghcr.io/umami-software/umami
`;

function check(source: string) {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  return { result, diagnostics: result.success ? [] : fromZod(result.error, locate) };
}

describe("manifestSchema", () => {
  it("accepts a minimal template and fills the defaults", () => {
    const { result } = check(minimal);

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.environment).toBe("production");
    expect(result.data.databases).toEqual([]);
    expect(result.data.apps[0].key).toBe("web");
  });

  it("refuses an unknown key and suggests the real one", () => {
    const { diagnostics } = check(`${minimal}    healthcheck: /up\n`);

    expect(diagnostics[0].code).toBe("schema.unknown-key");
    expect(diagnostics[0].message).toContain("healthcheck");
    expect(diagnostics[0].hint).toContain("health");
    expect(diagnostics[0].range?.start.line).toBe(6);
  });

  it("does not suggest env for the unrelated environment", () => {
    const { diagnostics } = check(`${minimal}    environment: staging\n`);

    expect(diagnostics[0].code).toBe("schema.unknown-key");
    expect(diagnostics[0].hint ?? "").not.toContain("env");
  });

  it("refuses a version it does not speak", () => {
    const { diagnostics } = check(minimal.replace("version: 1", "version: 2"));

    expect(diagnostics[0].path).toEqual(["version"]);
  });

  it("requires at least one app", () => {
    const { result } = check("version: 1\nproject: umami\napps: []\n");

    expect(result.success).toBe(false);
  });

  it("reads an environment variable written as a number", () => {
    const { result } = check(`${minimal}    env:\n      PORT: 3000\n`);

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.apps[0].env).toEqual({ PORT: "3000" });
  });

  it("points a missing required field at its parent", () => {
    const { diagnostics } = check("version: 1\napps:\n  - key: web\n    image: nginx\n");

    expect(diagnostics.some((d) => d.path.join(".") === "project")).toBe(true);
  });
});
