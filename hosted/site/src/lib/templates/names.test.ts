import { describe, expect, it } from "vitest";
import { displayName, tagsOf } from "./names";

describe("displayName", () => {
  it("reads the app out of the naming convention", () => {
    expect(displayName("cubeship-umami-template")).toBe("Umami");
    expect(displayName("cubeship-uptime-kuma-template")).toBe("Uptime Kuma");
  });

  it("leaves a name outside the convention readable", () => {
    expect(displayName("plausible")).toBe("Plausible");
    expect(displayName("my_app")).toBe("My App");
  });
});

describe("tagsOf", () => {
  it("drops the topic every template carries", () => {
    expect(tagsOf(["cubeship-template", "analytics"])).toEqual(["analytics"]);
  });
});
