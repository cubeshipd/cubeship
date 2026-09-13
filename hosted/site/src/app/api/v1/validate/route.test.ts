import { describe, expect, it } from "vitest";
import { POST } from "./route";

async function post(body: string, type = "text/yaml") {
  const response = await POST(
    new Request("http://localhost/api/v1/validate", {
      method: "POST",
      headers: { "content-type": type },
      body,
    }),
  );
  return { status: response.status, body: await response.json() };
}

describe("POST /api/v1/validate", () => {
  it("returns the manifest for a valid file", async () => {
    const { status, body } = await post(
      "version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: '1'\n",
    );

    expect(status).toBe(200);
    expect(body.ok).toBe(true);
    expect(body.manifest.apps[0].name).toBe("web");
  });

  it("returns diagnostics with a 422 for a broken one", async () => {
    const { status, body } = await post("version: 1\napps: []\n");

    expect(status).toBe(422);
    expect(body.ok).toBe(false);
    expect(body.diagnostics.length).toBeGreaterThan(0);
  });

  it("refuses a body that is too large", async () => {
    const { status, body } = await post("x".repeat(256 * 1024 + 1));

    expect(status).toBe(413);
    expect(body.error.code).toBe("too_large");
  });
});
