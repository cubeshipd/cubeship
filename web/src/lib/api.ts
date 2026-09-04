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

export type RegistryProvider = "generic" | "digitalocean" | "aws";

export type RegistryCredential = {
  id: number;
  provider: RegistryProvider;
  host: string;
  // The path segment between the host and the image, where the provider
  // has one — DigitalOcean's registry name.
  namespace?: string;
  region?: string;
  username: string;
  created_at: string;
  updated_at: string;
};

// What a live probe of a registry found. `unauthorized` is fixed by
// storing a new login, which is what the registry's settings screen is
// for; `unreachable` is someone else's registry being down.
export type RegistryStatus = {
  state: "available" | "unauthorized" | "unreachable";
  detail?: string;
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
  // The GitHub App this instance acts as. Its credentials are
  // write-only: the daemon reports whether they are there, never what
  // they are.
  github_app_slug?: string;
  github_connected: boolean;
};

// GitHubInstallation is one GitHub account an organization has
// connected. The installation is what lets Cubeship clone that
// account's private repositories, and what makes a push to one deploy
// the apps built from it.
export type GitHubInstallation = {
  id: number;
  installation_id: number;
  account: string;
  created_at: string;
};

export type GitHubRepository = {
  full_name: string;
  private: boolean;
  default_branch: string;
};

export type GitHubBranch = { name: string };

export type GitHubConnections = {
  installations: GitHubInstallation[];
  // Where to send someone to install the App. Empty until the instance
  // is registered as one.
  install_url: string;
};

// ownerOf reads the GitHub account a repository URL belongs to, which is
// what an installation is matched on. Anything not on GitHub — or not a
// repository — is null, and none of this applies to it.
export function ownerOf(repo: string): string | null {
  const match = repo.trim().match(/^https?:\/\/(?:www\.)?github\.com\/([^/\s]+)\/([^/\s]+)/i);
  return match ? match[1] : null;
}

// One repository in a registry, and one tag in it. The same shape
// whichever registry answered — Cubeship's own or somebody else's —
// because what the dashboard shows is the same either way.
export type RegistryRepository = { name: string };

export type RegistryImage = {
  tag: string;
  digest?: string;
  size?: number;
  pushed_at?: string;
};

export type RegistryUsage = {
  total_bytes: number;
  // Always true: layers are shared between images, so two tags built
  // from one base count that base twice. An upper bound on what is
  // stored, not what is billed.
  counts_shared_layers: boolean;
  repositories: { name: string; bytes: number; images: number }[];
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
