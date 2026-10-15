import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const disk = vi.hoisted(() => new Map<string, string>());
vi.mock("node:fs/promises", () => ({
  mkdir: vi.fn(),
  rm: vi.fn(async (path: string) => {
    disk.delete(path);
  }),
  readFile: vi.fn(async (path: string) => {
    if (!disk.has(path)) throw Object.assign(new Error("missing"), { code: "ENOENT" });
    return disk.get(path);
  }),
  writeFile: vi.fn(async (path: string, value: string) => {
    disk.set(path, value);
  }),
  rename: vi.fn(async (from: string, to: string) => {
    disk.set(to, disk.get(from) ?? "");
    disk.delete(from);
  }),
}));

const snapshot = {
  version: 1,
  commit: "a".repeat(40),
  published_at: "2026-10-15T12:00:00Z",
  templates: [
    {
      slug: "umami",
      id: "-9007199254740999",
      name: "Umami",
      description: "Analytics",
      source: "version: 1",
      readme: "# Umami",
      icon: "iVBORw0KGgo=",
      manifest: { schema_version: 1, apps: [], inputs: [], databases: [], stores: [] },
    },
  ],
};

beforeEach(() => {
  vi.resetModules();
  disk.clear();
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("published catalog", () => {
  it("shares a refresh, preserves the last valid snapshot on failure and reloads it after restart", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(snapshot)));
    vi.stubGlobal("fetch", fetcher);
    let catalog = await import("./catalog-snapshot");
    const [first, second] = await Promise.all([
      catalog.catalogSnapshot(),
      catalog.catalogSnapshot(),
    ]);
    expect(first).toEqual(snapshot);
    expect(second).toBe(first);
    expect(fetcher).toHaveBeenCalledTimes(1);
    vi.resetModules();
    fetcher.mockRejectedValue(new Error("GitHub offline"));
    catalog = await import("./catalog-snapshot");
    expect(await catalog.catalogSnapshot()).toEqual(snapshot);
    vi.resetModules();
    fetcher.mockResolvedValue(new Response('{"version":1,"templates":[]}'));
    catalog = await import("./catalog-snapshot");
    expect(await catalog.catalogSnapshot()).toEqual(snapshot);
  });

  it("updates without a rebuild and keeps icons from previous snapshots", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(snapshot)));
    vi.stubGlobal("fetch", fetcher);
    const catalog = await import("./catalog-snapshot");
    await catalog.catalogSnapshot();
    vi.spyOn(Date, "now").mockReturnValue(Date.now() + 61_000);
    const next = { ...snapshot, commit: "b".repeat(40) };
    fetcher.mockResolvedValue(new Response(JSON.stringify(next)));
    expect((await catalog.catalogSnapshot()).commit).toBe(next.commit);
    expect(await catalog.catalogSnapshotAt(snapshot.commit)).toEqual(snapshot);
  });

  it("refuses an invalid first publication and duplicate identities", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("{}")));
    const catalog = await import("./catalog-snapshot");
    await expect(catalog.catalogSnapshot()).rejects.toThrow("unavailable");
    expect(() =>
      catalog.parseSnapshot(
        JSON.stringify({ ...snapshot, templates: [snapshot.templates[0], snapshot.templates[0]] }),
      ),
    ).toThrow();
  });
});
