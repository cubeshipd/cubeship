import { HttpError } from "@/lib/http";
import { currentUser, type SessionUser } from "./session";

export async function requireUser(): Promise<SessionUser> {
  const user = await currentUser();
  if (!user) throw new HttpError(401, "unauthenticated", "sign in with GitHub first");
  return user;
}

export async function requireAdmin(): Promise<SessionUser> {
  const user = await requireUser();
  if (user.role !== "admin") throw new HttpError(403, "forbidden", "this is for administrators");
  return user;
}
