import { describe, expect, it } from "vitest";
import { fail, HttpError } from "./http";

describe("fail", () => {
  it("maps an HttpError to its own status and code", async () => {
    const response = fail(new HttpError(422, "invalid_body", "bad"));

    expect(response.status).toBe(422);
    expect((await response.json()).error.code).toBe("invalid_body");
  });

  it("maps a connection failure to a fast 503, not a 500", async () => {
    const refused = Object.assign(new Error("connect ECONNREFUSED 10.0.0.1:5432"), {
      code: "ECONNREFUSED",
    });
    // drizzle wraps the real error in a DrizzleQueryError and puts the
    // original on `.cause` — this is that shape, not the pg error alone.
    const wrapped = new Error("Failed query: select 1", { cause: refused });

    const response = fail(wrapped);

    expect(response.status).toBe(503);
    expect((await response.json()).error.code).toBe("unavailable");
  });

  it("maps pg's own connection timeout message, which carries no code", async () => {
    const response = fail(new Error("Connection terminated due to connection timeout"));

    expect(response.status).toBe(503);
    expect((await response.json()).error.code).toBe("unavailable");
  });

  it("maps an unplanned error to 500", async () => {
    const response = fail(new Error("something else broke"));

    expect(response.status).toBe(500);
    expect((await response.json()).error.code).toBe("internal");
  });
});
