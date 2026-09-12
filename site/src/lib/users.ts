import { eq } from "drizzle-orm";
import { db } from "@/db/client";
import { users } from "@/db/schema";
import { adminLogins } from "@/lib/env";
import type { SessionUser } from "./auth/session";

export async function upsertGithubUser(account: {
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
}): Promise<SessionUser> {
  const role = adminLogins().includes(account.login.toLowerCase()) ? "admin" : "user";
  const [row] = await db()
    .insert(users)
    .values({
      githubId: account.id,
      login: account.login,
      name: account.name,
      avatarUrl: account.avatarUrl,
      role,
    })
    .onConflictDoUpdate({
      target: users.githubId,
      set: { login: account.login, name: account.name, avatarUrl: account.avatarUrl, role },
    })
    .returning();

  return {
    id: row.id,
    login: row.login,
    name: row.name,
    avatarUrl: row.avatarUrl,
    role: row.role as "user" | "admin",
  };
}

export async function findByLogin(login: string) {
  const [row] = await db().select().from(users).where(eq(users.login, login)).limit(1);
  return row;
}
