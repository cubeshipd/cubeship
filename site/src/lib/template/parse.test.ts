import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";

describe("parseSource", () => {
  it("returns the parsed value", () => {
    const { value, diagnostics } = parseSource("project: umami\napps: []\n");

    expect(diagnostics).toEqual([]);
    expect(value).toEqual({ project: "umami", apps: [] });
  });

  it("reports a syntax error with a position", () => {
    const { value, diagnostics } = parseSource("project: umami\n  bad: indent\n");

    expect(value).toBeUndefined();
    expect(diagnostics[0].severity).toBe("error");
    expect(diagnostics[0].code).toBe("yaml.syntax");
    expect(diagnostics[0].range?.start.line).toBe(2);
  });

  it("strips yaml's own position and caret snippet from the message", () => {
    const { diagnostics } = parseSource("project: [oops\n");

    expect(diagnostics[0].message).not.toMatch(/line/i);
    expect(diagnostics[0].message).not.toMatch(/\^/);
    expect(diagnostics[0].range?.start.line).toBe(2);
  });

  it("keeps two independent syntax errors as two diagnostics", () => {
    const { diagnostics } = parseSource("a: 1\na: 2\nb: 1\nb: 2\n");

    expect(diagnostics.length).toBe(2);
    expect(diagnostics[0].range?.start.line).not.toBe(diagnostics[1].range?.start.line);
  });

  it("locates a value by path", () => {
    const { locate } = parseSource("apps:\n  - name: web\n    port: 3000\n");
    const range = locate(["apps", 0, "port"]);

    expect(range?.start.line).toBe(3);
  });

  it("locates a key when asked", () => {
    const { locate } = parseSource("apps:\n  - port: 3000\n");
    const key = locate(["apps", 0, "port"], "key");
    const value = locate(["apps", 0, "port"], "value");

    expect(key?.start.column).toBe(5);
    expect(value?.start.column).toBe(11);
  });

  it("falls back to the parent when the path is absent", () => {
    const { locate } = parseSource("apps:\n  - name: web\n");

    expect(locate(["apps", 0, "port"])?.start.line).toBe(2);
  });

  it("gives an empty document one diagnostic and no value", () => {
    const { value, diagnostics } = parseSource("   \n");

    expect(value).toBeUndefined();
    expect(diagnostics[0].code).toBe("yaml.empty");
  });
});
