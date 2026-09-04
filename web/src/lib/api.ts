// The dashboard is served by the daemon it talks to, so the API is
// always the same origin under one prefix. Nothing here takes a base
// URL: there is no deployment where those two come apart.
const PREFIX = "/api";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(PREFIX + path, {
    method,
    // The session is a cookie the daemon set. Sending it is the whole
    // of the dashboard's authentication.
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (!res.ok) {
    // Errors are text/plain: that is what http.Error writes.
    throw new ApiError(res.status, (await res.text()).trim() || res.statusText);
  }
  if (!res.headers.get("Content-Type")?.includes("json")) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {}),
  put: <T>(path: string, body: unknown) => request<T>("PUT", path, body),
  patch: <T>(path: string, body: unknown) => request<T>("PATCH", path, body),
  del: <T>(path: string) => request<T>("DELETE", path),
};

export type SetupStatus = { needed: boolean };
export type Me = { username: string; is_super_admin: boolean };
export type Org = { slug: string; name: string };
export type Project = {
  slug: string;
  name: string;
  description: string;
  environments?: string[];
};
export type Environment = { slug: string; name: string; description: string };

// registry and external run a published image; dockerfile and railpack
// build one from a Git repository, which is why they need an admin.
export type AppSource = "registry" | "external" | "dockerfile" | "railpack";

export const BUILDING_SOURCES: AppSource[] = ["dockerfile", "railpack"];

export type App = {
  reference: string;
  name: string;
  description: string;
  // Empty until someone configures it, and required before the app can
  // deploy — which is what makes a freshly created app undeployable.
  domain: string;
  // For a registry app, where to push; for an external one, what it pulls.
  image?: string;
  status: string;
  // The daemon's four. The dashboard groups them into two: an app is
  // built from a repository, or it runs an image someone published.
  source: AppSource;
  // Where a building app builds from. Absent for one that does not.
  repo?: string;
  ref?: string;
  dockerfile?: string;
  org: string;
  project: string;
  environment: string;
};

export type RegistryCredential = {
  id: number;
  name: string;
  host: string;
  username: string;
  created_at: string;
  updated_at: string;
};

export type Deployment = {
  id: number;
  status: string;
  image: string;
  error?: string;
  created_at: string;
};

export type Settings = {
  domain: string;
  acme_email: string;
  registry_host?: string;
  tls_enabled: boolean;
};

export type ApiKey = {
  id: number;
  name: string;
  created_at: string;
  last_used_at?: string;
  current_key: boolean;
};

export type ResolvedVar = { key: string; value: string; source: string };
export type EnvView = { vars: Record<string, string>; effective?: ResolvedVar[] };
