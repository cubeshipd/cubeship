import { describe, expect, it } from "vitest";
import { attributesFor, findReferences, internalHost } from "./references";

describe("findReferences", () => {
  it("finds one reference and its parts", () => {
    expect(findReferences("https://${input.domain}/x")).toEqual([
      { kind: "input", key: "domain", attr: undefined, raw: "${input.domain}", start: 8 },
    ]);
  });

  it("finds several, with attributes", () => {
    const found = findReferences("${db.main.host}:${db.main.port}");

    expect(found.map((r) => r.attr)).toEqual(["host", "port"]);
    expect(found.every((r) => r.kind === "db")).toBe(true);
  });

  it("ignores what is not a reference", () => {
    expect(findReferences("$ {input.x} and ${nope}")).toEqual([]);
  });
});

describe("attributesFor", () => {
  it("gives an app three and a store two", () => {
    expect(attributesFor.app).toEqual(["internal", "host", "port"]);
    expect(attributesFor.store).toEqual(["bucket", "endpoint"]);
    expect(attributesFor.input).toEqual([]);
  });
});

describe("internalHost", () => {
  it("spells the alias the daemon attaches", () => {
    expect(internalHost("umami", "production", "web")).toBe("cubeship-umami-production-web");
  });
});
