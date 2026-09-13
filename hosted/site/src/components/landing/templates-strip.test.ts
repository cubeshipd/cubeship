import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/templates/queries", () => ({
  listTemplates: vi.fn().mockRejectedValue(new Error("connect ETIMEDOUT 10.0.0.1:5432")),
}));

describe("TemplatesStrip", () => {
  it("renders nothing when the catalog query fails", async () => {
    const { TemplatesStrip } = await import("./templates-strip");

    expect(await TemplatesStrip()).toBeNull();
  });
});
