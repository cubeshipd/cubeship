import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";
import { manifestSchema } from "./schema";
import { checkSemantics } from "./semantics";

function codes(source: string): string[] {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  if (!result.success) throw new Error(`the fixture does not parse: ${result.error.message}`);
  return checkSemantics(result.data, locate).map((d) => d.code);
}

const ok = `version: 1
project: demo
inputs:
  - key: domain
    type: domain
    label: Where it answers
databases:
  - key: db
    engine: postgres
    version: "18"
    database: analytics
apps:
  - key: web
    image: ghcr.io/umami-software/umami
    tag: postgresql-v2
    port: 3000
    health: /api/heartbeat
    domains:
      - host: \${input.domain}
    attach:
      - database: db
    limits:
      cpu: 1
      memory: 1Gi
`;

describe("checkSemantics", () => {
  it("passes a template that is right", () => {
    expect(codes(ok)).toEqual([]);
  });

  it("refuses a literal host", () => {
    expect(codes(ok.replace("${input.domain}", "analytics.example.com"))).toContain(
      "domain.literal",
    );
  });

  it("refuses a reference to something undeclared", () => {
    expect(codes(ok.replace("${input.domain}", "${input.nope}"))).toContain("reference.unknown");
  });

  it("refuses an attribute a kind does not have", () => {
    expect(codes(`${ok}    env:\n      URL: \${db.db.endpoint}\n`)).toContain(
      "reference.attribute",
    );
  });

  it("refuses a tag inside the image", () => {
    expect(codes(ok.replace("umami\n", "umami:latest\n"))).toContain("image.tagged");
  });

  it("refuses two databases at one prefix on one app", () => {
    const source = ok
      .replace(
        "    database: analytics\n",
        "    database: analytics\n  - key: other\n    engine: postgres\n",
      )
      .replace("      - database: db\n", "      - database: db\n      - database: other\n");
    expect(codes(source)).toContain("attach.collision");
  });

  it("refuses an engine version the daemon does not have", () => {
    expect(codes(ok.replace('version: "18"', 'version: "9"'))).toContain("engine.version");
  });

  it("refuses a user the engine refuses", () => {
    const mysql = ok
      .replace("engine: postgres", "engine: mysql")
      .replace('version: "18"', 'version: "8.4"');
    expect(
      codes(
        mysql.replace("    database: analytics", "    database: analytics\n    username: root"),
      ),
    ).toContain("engine.username");
  });

  it("refuses a memory that is not a size, and one below the floor", () => {
    expect(codes(ok.replace("1Gi", "loads"))).toContain("limits.memory");
    expect(codes(ok.replace("1Gi", "1Ki"))).toContain("limits.memory");
  });

  it("refuses duplicate keys and reserved names", () => {
    expect(
      codes(ok.replace("  - key: web", "  - key: web\n    image: nginx\n  - key: web")),
    ).toContain("key.duplicate");
    expect(codes(ok.replace("  - key: db\n", "  - key: db\n    name: engines\n"))).toContain(
      "name.reserved",
    );
  });

  it("refuses an app with no source and one with two", () => {
    expect(codes(ok.replace("    image: ghcr.io/umami-software/umami\n", ""))).toContain(
      "source.missing",
    );
    expect(
      codes(
        ok.replace(
          "    port: 3000",
          "    repo: https://github.com/x/y\n    build: railpack\n    port: 3000",
        ),
      ),
    ).toContain("source.conflict");
  });
});
