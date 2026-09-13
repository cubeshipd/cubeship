import { describe, expect, it, vi } from "vitest";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { upsertGithubUser } from "./users";

describe.skipIf(!testDatabaseUrl)("upsertGithubUser", () => {
  it("creates once and updates afterwards", async () => {
    await withDatabase();
    const first = await upsertGithubUser({ id: 1, login: "lucas", name: "Lucas", avatarUrl: null });
    const second = await upsertGithubUser({
      id: 1,
      login: "lucas-l",
      name: "Lucas L",
      avatarUrl: "u",
    });

    expect(second.id).toBe(first.id);
    expect(second.login).toBe("lucas-l");
  });

  it("makes the listed logins admin", async () => {
    await withDatabase();
    vi.stubEnv("ADMIN_LOGINS", "lucas");
    const user = await upsertGithubUser({ id: 2, login: "Lucas", name: null, avatarUrl: null });

    expect(user.role).toBe("admin");
    vi.unstubAllEnvs();
  });
});
