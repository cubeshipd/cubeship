import { describe, expect, it } from "vitest";
import { healthPathProblem, parseSize, prefixPattern, slugPattern } from "./values";

describe("parseSize", () => {
  it("reads the spellings the CLI reads", () => {
    expect(parseSize("512Mi")).toBe(536870912);
    expect(parseSize("512M")).toBe(536870912);
    expect(parseSize("2Gi")).toBe(2147483648);
    expect(parseSize("1500")).toBe(1500);
    expect(parseSize(1500)).toBe(1500);
  });

  it("refuses what is not a size", () => {
    expect(parseSize("big")).toBeUndefined();
    expect(parseSize("")).toBeUndefined();
  });
});

describe("slugPattern", () => {
  it("matches the daemon's shape", () => {
    expect(slugPattern.test("umami-db")).toBe(true);
    expect(slugPattern.test("Umami")).toBe(false);
    expect(slugPattern.test("-db")).toBe(false);
    expect(slugPattern.test("db-")).toBe(false);
  });
});

describe("prefixPattern", () => {
  it("requires upper case ending in an underscore", () => {
    expect(prefixPattern.test("ANALYTICS_")).toBe(true);
    expect(prefixPattern.test("analytics_")).toBe(false);
    expect(prefixPattern.test("ANALYTICS")).toBe(false);
  });
});

describe("healthPathProblem", () => {
  it("accepts a path", () => {
    expect(healthPathProblem("/api/heartbeat")).toBeUndefined();
  });

  it("names what is wrong", () => {
    expect(healthPathProblem("api")).toMatch(/start with/);
    expect(healthPathProblem("/a?b=1")).toMatch(/query/);
    expect(healthPathProblem(`/${"a".repeat(255)}`)).toMatch(/255/);
  });
});
