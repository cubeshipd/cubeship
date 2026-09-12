import { describe, expect, it } from "vitest";
import { advise } from "./advice";
import { parseSource } from "./parse";
import { manifestSchema } from "./schema";

function codes(source: string): string[] {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  if (!result.success) throw new Error("the fixture does not parse");
  return advise(result.data, locate).map((d) => d.code);
}

const bare = `version: 1
project: demo
apps:
  - key: web
    image: nginx
`;

describe("advise", () => {
  it("warns about everything a bare app leaves out", () => {
    const found = codes(bare);

    expect(found).toContain("advice.no-health");
    expect(found).toContain("advice.floating-tag");
    expect(found).toContain("advice.no-limits");
    expect(found).toContain("advice.unreachable-app");
    expect(found).toContain("advice.dockerhub");
  });

  it("warns about an engine left unpinned", () => {
    expect(codes(`${bare}databases:\n  - key: db\n    engine: postgres\n`)).toContain(
      "advice.unpinned-engine",
    );
  });

  it("never returns an error", () => {
    const { value, locate } = parseSource(bare);
    const result = manifestSchema.safeParse(value);
    if (!result.success) throw new Error("the fixture does not parse");

    expect(advise(result.data, locate).every((d) => d.severity !== "error")).toBe(true);
  });
});
