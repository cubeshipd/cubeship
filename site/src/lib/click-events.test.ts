import { describe, expect, it } from "vitest";
import { eventData } from "./click-events";

describe("eventData", () => {
  it("names the event and keeps only the data-track-* properties", () => {
    expect(eventData({ track: "install", trackLocation: "hero", state: "open" })).toEqual({
      name: "install",
      data: { location: "hero" },
    });
  });

  it("ignores an element with no event name", () => {
    expect(eventData({ track: "", trackLocation: "hero" })).toBeUndefined();
    expect(eventData({ trackLocation: "hero" })).toBeUndefined();
  });
});
