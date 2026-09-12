import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "@/lib/templates/publish";
import { GET } from "./route";

const source = `version: 1
project: demo
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
`;

async function seed() {
  const handle = await withDatabase();
  const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };
  const { slug } = await createDraft(user, { name: "Demo", summary: "a demo template", tags: [] });
  await publishVersion(user, slug, { source });
  await publishVersion(user, slug, { source: source.replace('"1"', '"2"') });
  return slug;
}

function request(slug: string, query = "") {
  return GET(new Request(`http://localhost/api/v1/templates/${slug}/manifest${query}`), {
    params: Promise.resolve({ slug }),
  });
}

describe.skipIf(!testDatabaseUrl)("GET /api/v1/templates/{slug}/manifest", () => {
  it("serves the newest version by default", async () => {
    const slug = await seed();
    const body = await (await request(slug)).json();

    expect(body.schema_version).toBe(1);
    expect(body.apps[0].source.tag).toBe("2");
  });

  it("serves a pinned version", async () => {
    const slug = await seed();
    const body = await (await request(slug, "?version=1")).json();

    expect(body.apps[0].source.tag).toBe("1");
  });

  it("404s for an unknown template and an unknown version", async () => {
    const slug = await seed();

    expect((await request("nope")).status).toBe(404);
    expect((await request(slug, "?version=9")).status).toBe(404);
  });

  it("allows cross-origin reads, because an instance will do one", async () => {
    const slug = await seed();

    expect((await request(slug)).headers.get("access-control-allow-origin")).toBe("*");
  });
});
