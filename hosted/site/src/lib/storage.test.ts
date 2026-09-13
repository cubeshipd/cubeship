import { describe, expect, it } from "vitest";
import { type ObjectClient, putImage } from "./storage";

function memoryClient(): ObjectClient & {
  store: Map<string, { body: Uint8Array; contentType: string }>;
} {
  const store = new Map<string, { body: Uint8Array; contentType: string }>();
  return {
    store,
    async put(key, body, contentType) {
      store.set(key, { body, contentType });
    },
    async get(key) {
      const found = store.get(key);
      if (!found) throw new Error("missing");
      return found;
    },
  };
}

// A 2x2 PNG, so the test needs no fixture file on disk.
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAACXBIWXMAAAPoAAAD6AG1e1JrAAAAEklEQVQImWPgOvGL68QvBggFAC6eBzGBpTOQAAAAAElFTkSuQmCC",
  "base64",
);

describe("putImage", () => {
  it("re-encodes to WebP under a generated key", async () => {
    const client = memoryClient();
    const key = await putImage(png, client);

    expect(key).toMatch(/^templates\/[0-9a-f-]{36}\.webp$/);
    expect(client.store.get(key)?.contentType).toBe("image/webp");
    expect(client.store.get(key)?.body.subarray(8, 12).toString()).toBe("WEBP");
  });

  it("refuses what is not an image", async () => {
    await expect(putImage(Buffer.from("not an image"), memoryClient())).rejects.toThrow(/image/);
  });

  it("refuses a file over the limit", async () => {
    await expect(putImage(Buffer.alloc(2 * 1024 * 1024 + 1), memoryClient())).rejects.toThrow(
      /2 MB/,
    );
  });
});
