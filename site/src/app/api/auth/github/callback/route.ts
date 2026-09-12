import { cookies } from "next/headers";
import { exchangeCode, readAccount, STATE_COOKIE } from "@/lib/auth/github";
import { createSession } from "@/lib/auth/session";
import { publicUrl } from "@/lib/env";
import { HttpError, route } from "@/lib/http";
import { upsertGithubUser } from "@/lib/users";

export const GET = route(async (request) => {
  const url = new URL(request.url);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");

  const jar = await cookies();
  const stored = jar.get(STATE_COOKIE)?.value;
  jar.delete(STATE_COOKIE);

  // Split on the first colon only: "next" is a path and may carry its own.
  const separator = (stored ?? "").indexOf(":");
  const expected = separator === -1 ? (stored ?? "") : (stored ?? "").slice(0, separator);
  const next = separator === -1 ? "/templates" : (stored ?? "").slice(separator + 1);
  // A mismatch is somebody else starting the flow, not a slip.
  if (!code || !state || !expected || state !== expected) {
    throw new HttpError(400, "bad_state", "that sign-in did not start here; try again");
  }

  const token = await exchangeCode(code);
  const account = await readAccount(token);
  // The token has done its one job. Keeping it would be holding a
  // credential with nothing to spend it on.
  const user = await upsertGithubUser(account);
  await createSession(user.id);

  return Response.redirect(new URL(next, publicUrl()).toString(), 302);
});
