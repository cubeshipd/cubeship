import { describe, expect, it } from "vitest";
import { templateJsonSchema } from "./jsonschema";

describe("templateJsonSchema", () => {
  it("describes the manifest and refuses extra keys", () => {
    const schema = templateJsonSchema() as Record<string, any>;

    expect(schema.$schema).toContain("json-schema.org");
    expect(schema.properties.apps).toBeDefined();
    expect(schema.properties.project).toBeDefined();
    expect(schema.additionalProperties).toBe(false);
  });
});
