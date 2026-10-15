import { describe, expect, it, vi } from "vitest";
import { GET } from "@/app/api/v1/[...path]/route";
import { GET as GETv2 } from "@/app/api/v2/templates/[[...path]]/route";
import { GET as GETicons } from "@/app/api/v2/icons/[...path]/route";
import { GET as GETtags } from "@/app/api/v2/tags/route";
import { listTemplates } from "./catalog";

vi.mock("./catalog-snapshot", () => {
  const snapshot = {
    version: 1,
    commit: "a".repeat(40),
    published_at: "2026-10-15T12:00:00Z",
    templates: [
      { slug: "umami", id: "-9007199254740999", name: "Umami", description: "Analytics" },
      { slug: "grafana", id: "-9007199254740998", name: "Grafana", description: "Monitoring" },
    ].map((entry) => ({
      ...entry,
      source: "version: 1",
      readme: `# ${entry.name}`,
      icon: "iVBORw0KGgo=",
      manifest: { schema_version: 1, apps: [] },
    })),
  };
  return {
    catalogSnapshot: async (version = 2) => ({ ...snapshot, version }),
    catalogSnapshotAt: async (sha: string, version = 2) =>
      sha === snapshot.commit ? { ...snapshot, version } : null,
  };
});

const request = (path: string) =>
  GET(new Request(`https://cubeship.dev/api/v1/${path}`), {
    params: Promise.resolve({ path: path.split("?")[0].split("/") }),
  });

describe("catalog v1 compatibility", () => {
  it("searches and pages without rounding legacy int64 IDs", async () => {
    const first = await listTemplates({ limit: 1 });
    expect(first.templates[0].name).toBe("grafana");
    const second = await listTemplates({ limit: 1, cursor: first.next_cursor ?? undefined });
    expect(second.templates[0].name).toBe("umami");
    expect(second.next_cursor).toBeNull();
    expect((await listTemplates({ q: "analytics" })).templates[0].name).toBe("umami");
    expect((await listTemplates({ tag: "old-topic" })).templates).toEqual([]);
  });
  it("keeps detail, manifest, icon, CORS and error contracts", async () => {
    const detail = await request("templates/cubeshipd/umami");
    expect(detail.headers.get("access-control-allow-origin")).toBe("*");
    expect(await detail.json()).toMatchObject({
      owner: "cubeshipd",
      name: "umami",
      verified: true,
      release: { tag: "a".repeat(40), commit: "a".repeat(40) },
      source_url: `https://raw.githubusercontent.com/cubeshipd/cubeship-templates/${"a".repeat(40)}/umami/template.yaml`,
      icon_url: `https://cubeship.dev/api/v1/icons/-9007199254740999/${"a".repeat(40)}.png`,
    });
    expect((await request("templates/cubeshipd/umami/manifest")).status).toBe(200);
    const icon = await request(`icons/-9007199254740999/${"a".repeat(40)}.png`);
    expect(icon.headers.get("content-type")).toBe("image/png");
    expect(icon.headers.get("cache-control")).toContain("immutable");
    expect((await request("templates/cubeshipd/umami/releases")).status).toBe(404);
    expect((await request("templates/cubeshipd/missing")).status).toBe(404);
    for (const path of [
      "templates?cursor=garbage",
      "templates?limit=0",
      "templates?limit=49",
      "templates?sort=bad",
      "templates/cubeshipd/umami/manifest?release=v1",
    ]) {
      const response = await request(path);
      expect(response.status).toBe(400);
      expect(response.headers.get("cache-control")).toBe("no-store");
    }
  });
});

it("v2 names the main repository while v1 keeps its original source", async () => {
  const response = await GETv2(
    new Request("https://cubeship.dev/api/v2/templates/cubeshipd/umami"),
    { params: Promise.resolve({ path: ["cubeshipd", "umami"] }) },
  );
  expect(await response.json()).toMatchObject({
    source_url: `https://raw.githubusercontent.com/cubeshipd/cubeship/${"a".repeat(40)}/templates/umami/template.yaml`,
    url: `https://github.com/cubeshipd/cubeship/tree/${"a".repeat(40)}/templates/umami`,
    icon_url: `https://cubeship.dev/api/v2/icons/-9007199254740999/${"a".repeat(40)}.png`,
  });
});

it("v2 exposes the template list and explicit tags and icon routes", async () => {
  const response = await GETv2(new Request("https://cubeship.dev/api/v2/templates"), {
    params: Promise.resolve({}),
  });
  expect((await response.json()).templates).toHaveLength(2);
  expect(await (await GETtags(new Request("https://cubeship.dev/api/v2/tags"))).json()).toEqual({
    tags: [],
  });
  const file = `${"a".repeat(40)}.png`;
  const icon = await GETicons(
    new Request(`https://cubeship.dev/api/v2/icons/-9007199254740999/${file}`),
    {
      params: Promise.resolve({ path: ["-9007199254740999", file] }),
    },
  );
  expect(icon.headers.get("content-type")).toBe("image/png");
  expect(icon.status).toBe(200);
});
