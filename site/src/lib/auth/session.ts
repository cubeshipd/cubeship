import { createHash, randomBytes } from "node:crypto";
import { and, eq, gt } from "drizzle-orm";
import { cookies } from "next/headers";
import { db } from "@/db/client";
import { sessions, users } from "@/db/schema";

export const SESSION_COOKIE = "cubeship_session";
const THIRTY_DAYS = 30 * 24 * 60 * 60;

export type SessionUser = {
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
  role: "user" | "admin";
};

function hash(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

export async function createSession(userId: number): Promise<{ token: string; expiresAt: Date }> {
  const token = randomBytes(32).toString("base64url");
  const expiresAt = new Date(Date.now() + THIRTY_DAYS * 1000);

  await db()
    .insert(sessions)
    .values({ tokenHash: hash(token), userId, expiresAt });
  const jar = await cookies();
  jar.set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: THIRTY_DAYS,
  });

  return { token, expiresAt };
}

export async function currentUser(): Promise<SessionUser | null> {
  const token = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!token) return null;

  const [row] = await db()
    .select({
      id: users.id,
      login: users.login,
      name: users.name,
      avatarUrl: users.avatarUrl,
      role: users.role,
      blockedAt: users.blockedAt,
    })
    .from(sessions)
    .innerJoin(users, eq(users.id, sessions.userId))
    .where(and(eq(sessions.tokenHash, hash(token)), gt(sessions.expiresAt, new Date())))
    .limit(1);

  if (!row || row.blockedAt) return null;
  return {
    id: row.id,
    login: row.login,
    name: row.name,
    avatarUrl: row.avatarUrl,
    role: row.role as "user" | "admin",
  };
}

export async function destroySession(): Promise<void> {
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE)?.value;
  if (token)
    await db()
      .delete(sessions)
      .where(eq(sessions.tokenHash, hash(token)));
  jar.delete(SESSION_COOKIE);
}
