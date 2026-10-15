import assert from "node:assert/strict";
import { test } from "node:test";
import { selectableImageTags } from "./image-tags.ts";

test("image tag choices follow publication dates and omit SHA tags", () => {
  const tags = [
    { tag: "v9", created_at: "2026-01-01T00:00:00Z" },
    { tag: "sha-a1b2c3d", pushed_at: "2026-09-01T00:00:00Z" },
    { tag: "v2", created_at: "2026-08-01T00:00:00Z" },
    { tag: "sha_1234567" },
    { tag: "sha256-8a83cd0a355addff696ffbd797bf046d99a78c1bf8c45f354159465333ff168f" },
    { tag: "undated" },
    { tag: "also-undated" },
  ];
  assert.deepEqual(
    selectableImageTags(tags).map((image) => image.tag),
    ["v2", "v9", "undated", "also-undated"],
  );
});
