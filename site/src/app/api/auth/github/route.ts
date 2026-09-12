import { randomBytes } from "node:crypto";
import { cookies } from "next/headers";
import { authorizeUrl, STATE_COOKIE } from "@/lib/auth/github";
import { route } from "@/lib/http";

export const GET = route(async (request) => {
  const state = randomBytes(16).toString("base64url");
  const next = new URL(request.url).searchParams.get("next") ?? "/templates";

  const jar = await cookies();
  jar.set(STATE_COOKIE, `${state}:${next}`, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 600,
  });

  return Response.redirect(authorizeUrl(state), 302);
});
