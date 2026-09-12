import { describe, expect, it } from "vitest";
import { databaseVarNames, findEngine, storeVarNames } from "./engines";

describe("findEngine", () => {
  it("knows the five engines and their newest version first", () => {
    expect(findEngine("postgres")?.versions[0]).toBe("18");
    expect(findEngine("mariadb")?.versions).toContain("10.11");
    expect(findEngine("sqlite")).toBeUndefined();
  });
});

describe("databaseVarNames", () => {
  it("writes six names for an engine with databases", () => {
    const engine = findEngine("postgres");
    if (!engine) throw new Error("postgres is missing");

    expect(databaseVarNames(engine, "")).toEqual([
      "DATABASE_URL",
      "DATABASE_HOST",
      "DATABASE_PORT",
      "DATABASE_USER",
      "DATABASE_PASSWORD",
      "DATABASE_NAME",
    ]);
  });

  it("leaves the name out for Redis and honours the prefix", () => {
    const engine = findEngine("redis");
    if (!engine) throw new Error("redis is missing");

    expect(databaseVarNames(engine, "CACHE_")).toEqual([
      "CACHE_REDIS_URL",
      "CACHE_REDIS_HOST",
      "CACHE_REDIS_PORT",
      "CACHE_REDIS_USER",
      "CACHE_REDIS_PASSWORD",
    ]);
  });
});

describe("storeVarNames", () => {
  it("writes the six S3 names, never AWS_", () => {
    expect(storeVarNames("")).toEqual([
      "S3_ENDPOINT",
      "S3_REGION",
      "S3_BUCKET",
      "S3_ACCESS_KEY_ID",
      "S3_SECRET_ACCESS_KEY",
      "S3_PATH_STYLE",
    ]);
  });
});
