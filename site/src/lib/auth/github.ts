import { githubOAuth, publicUrl } from "@/lib/env";

export const STATE_COOKIE = "cubeship_oauth_state";

function redirectUri(): string {
  return `${publicUrl()}/api/auth/github/callback`;
}

// No scope: a template author's public identity is all this needs, and a
// token with reach is a liability we would then have to keep.
export function authorizeUrl(state: string): string {
  const url = new URL("https://github.com/login/oauth/authorize");
  url.searchParams.set("client_id", githubOAuth().clientId);
  url.searchParams.set("redirect_uri", redirectUri());
  url.searchParams.set("scope", "");
  url.searchParams.set("state", state);
  return url.toString();
}

export async function exchangeCode(code: string): Promise<string> {
  const { clientId, clientSecret } = githubOAuth();
  const response = await fetch("https://github.com/login/oauth/access_token", {
    method: "POST",
    headers: { accept: "application/json", "content-type": "application/json" },
    body: JSON.stringify({
      client_id: clientId,
      client_secret: clientSecret,
      code,
      redirect_uri: redirectUri(),
    }),
  });

  const body = (await response.json()) as { access_token?: string; error?: string };
  if (!body.access_token) throw new Error(body.error ?? "GitHub returned no token");
  return body.access_token;
}

export async function readAccount(token: string): Promise<{
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
}> {
  const response = await fetch("https://api.github.com/user", {
    headers: { authorization: `Bearer ${token}`, accept: "application/vnd.github+json" },
  });
  if (!response.ok) throw new Error(`GitHub answered ${response.status}`);

  const body = (await response.json()) as {
    id: number;
    login: string;
    name?: string;
    avatar_url?: string;
  };
  return {
    id: body.id,
    login: body.login,
    name: body.name ?? null,
    avatarUrl: body.avatar_url ?? null,
  };
}
