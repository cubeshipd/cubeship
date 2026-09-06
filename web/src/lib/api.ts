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

// token_required says the claim has to carry the token the installer
// printed — see setup.Token on the daemon.
export type SetupStatus = { needed: boolean; token_required: boolean };
export type Me = { username: string; is_super_admin: boolean };
// None of these has a display name. The slug is the name — the rule an
// app has always followed, now everywhere: a slug is a path component of
// every registry reference underneath it, so it is the identifier that
// cannot change and the one everybody reads.
export type Org = { slug: string };
export type Project = {
  slug: string;
  description: string;
  environments?: string[];
};
export type Environment = { slug: string; description: string };

// registry and external run a published image; dockerfile and railpack
// build one from a Git repository, which is why they need an admin.
export type AppSource = "registry" | "external" | "dockerfile" | "railpack";

export const BUILDING_SOURCES: AppSource[] = ["dockerfile", "railpack"];

// One name an app is served at.
//
// The pair is the unit: an image can expose several ports, so "which
// port does this app listen on" stops having one answer as soon as the
// app has more than one name.
export type AppDomain = {
  id: number;
  host: string;
  // What this name reaches inside the container, or 0 for "read it from
  // the image" — which is the normal answer.
  port: number;
};

// hostsOf renders every name an app answers at, for the places that have
// room for one line. "no domain" rather than an empty string, because
// answering nowhere is a state worth reading — a normal one for a worker
// or a queue consumer, and a surprise for anything meant to be visited.
export function hostsOf(app: { domains: AppDomain[] }): string {
  if (app.domains.length === 0) return "no domain";
  return app.domains.map((d) => d.host).join(", ");
}

export type App = {
  reference: string;
  name: string;
  description: string;
  // Every name this app answers at, each with the port behind it. Empty
  // is a normal state: an app nothing outside the instance should reach
  // deploys with none, and its neighbours reach it by container name.
  domains: AppDomain[];
  // A name this app could answer at, under the instance's own domain.
  // Only ever offered — nothing assigns it. Absent while the instance
  // has no domain to build one under.
  suggested_host?: string;
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

export type DNSProvider = "cloudflare" | "route53";

export type DNSCredential = {
  id: number;
  provider: DNSProvider;
  // What tells two accounts on one provider apart. Unique on the instance.
  label: string;
  // Route 53's access key id. Absent for Cloudflare, whose token is a
  // single value with no name attached to it.
  username?: string;
  created_at: string;
  updated_at: string;
};

export type DNSStatus = {
  state: "available" | "unauthorized" | "unreachable";
  detail?: string;
};

export type DNSZone = { id: string; name: string };

// A record is a list at both providers: two A records for one name are
// one record with two values here.
export type DNSRecord = {
  id?: string;
  name: string;
  type: string;
  values: string[];
  ttl: number;
  proxied?: boolean;
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
  // Whether the registered App can be installed anywhere but the
  // account that owns it. An App from before Cubeship asked for OAuth
  // on install was also registered private, and neither can be changed
  // after the fact — false means it has to be replaced, not fixed.
  github_oauth_ready?: boolean;
  // What this instance's DNS records should point at. The browser
  // cannot work this out, so the daemon reports it.
  public_ip?: string;
  public_ip_configured: boolean;
  // The stored DNS credential that writes this instance's own records.
  dns_provider_id?: string;
  tls_enabled: boolean;
  // The GitHub App this instance acts as. Its credentials are
  // write-only: the daemon reports whether they are there, never what
  // they are.
  github_app_slug?: string;
  github_connected: boolean;
  // Whether every name under the instance's domain already resolves
  // here — true of the sslip.io address a default install takes. It is
  // what decides whether a name for an app needs a record written.
  wildcard_domain?: boolean;
};

// GitHubInstallation is one GitHub account this instance has
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

// One TLS certificate this instance holds. Traefik issues and renews
// them; this is what it has, read out of its own store.
export type Certificate = {
  host: string;
  sans?: string[];
  issuer: string;
  not_before: string;
  not_after: string;
  serial: string;
  // The app served at that name, as its reference. Absent for the
  // instance's own names and for one nothing serves any more.
  app?: string;
  instance?: boolean;
  // Nothing on this instance answers there now. Still valid, just
  // unused.
  orphan?: boolean;
};

// Why a name this instance routes has no certificate. The three are
// three different jobs: configure the instance, redeploy the app, or
// look at what Traefik said.
export type MissingReason = "tls_not_configured" | "not_deployed" | "pending";

export type MissingCertificate = {
  host: string;
  app?: string;
  instance?: boolean;
  reason: MissingReason;
  // The last thing Traefik's log said about that name, when it said
  // anything. A quotation, not a contract.
  detail?: string;
};

export type CertificateReport = {
  tls_enabled: boolean;
  acme_email?: string;
  certificates: Certificate[];
  missing: MissingCertificate[];
};
