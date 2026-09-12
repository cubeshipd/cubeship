import { describe, expect, it } from "vitest";
import { validateTemplate } from "@/lib/template";
import { describeApp, describeDatabase } from "./preview";

const { manifest } = validateTemplate(`version: 1
project: umami
inputs:
  - key: domain
    type: domain
    label: Where it answers
databases:
  - key: db
    engine: postgres
    version: "18"
apps:
  - key: web
    image: nginx
    tag: "1"
    port: 3000
    domains:
      - host: \${input.domain}
    attach:
      - database: db
`);

describe("the preview's sentences", () => {
  it("says what an app is and where it answers", () => {
    if (!manifest) throw new Error("the fixture does not validate");

    expect(describeApp(manifest.apps[0], manifest.inputs)).toBe(
      "nginx:1 on port 3000, at the domain from “Where it answers”, reachable inside as cubeship-umami-production-web:3000",
    );
  });

  it("says what a database is", () => {
    if (!manifest) throw new Error("the fixture does not validate");

    expect(describeDatabase(manifest.databases[0])).toBe("postgres 18, named db, on port 5432");
  });
});
