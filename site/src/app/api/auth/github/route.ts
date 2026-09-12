import { randomBytes } from "node:crypto";
import { cookies } from "next/headers";
import { authorizeUrl, STATE_COOKIE } from "@/lib/auth/github";
import { route } from "@/lib/http";

// A same-site path only: "next" comes back verbatim in a redirect once
// sign-in succeeds, and an absolute URL there would make this GitHub's
// own OAuth flow laundering an open redirect to anywhere.
function sameSitePath(raw: string | null): string {
  if (raw?.startsWith("/") && !raw.startsWith("//")) return raw;
  return "/templates";
}

export const GET = route(async (request) => {
  const state = randomBytes(16).toString("base64url");
  const next = sameSitePath(new URL(request.url).searchParams.get("next"));

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
