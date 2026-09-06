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
// Who is signed in, from GET /users/me.
//
// `is_super_admin` used to be here, and it outlived the thing it named:
// organizations went, roles moved onto the account, and the daemon has
// answered with `role` ever since. Nothing complained, because reading
// a field that is not in the JSON is `undefined` rather than an error —
// so every admin-only piece of UI behind it was simply never rendered.
export type Me = {
  username: string;
  role: "admin" | "member";
  // Whether this account can sign in without an API key. It is what
  // says how much revoking the last key costs — see the account screen.
  has_password: boolean;
};
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

// hostsOf renders every name an app answers at, for the places that
// have room for one line.
//
// Empty when there are none, and the caller renders nothing. It used to
// say "no domain" on the grounds that answering nowhere is a state
// worth reading — but it is the *normal* state for a worker or a queue
// consumer, and a line of grey text under every one of them saying so
// is a caption on the ordinary.
export function hostsOf(app: { domains: AppDomain[] }): string {
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

// --- credentials ---

// One secret this instance holds. It carries no provider: what it is
// used for is the use's business, and the same key may be writing DNS
// records and pulling images at once. The secret itself is not here and
// cannot be asked for — the daemon is what talks to the provider, so
// nothing out here needs to read one back.
export type Credential = {
  id: number;
  label: string;
  // The first half, where the secret has one — an access key id, a
  // registry login. Not a secret: the secret is the other half.
  username?: string;
  // What is currently depending on it, so the list can say why one
  // cannot be deleted before somebody tries.
  in_use_by?: string[];
  created_at: string;
  updated_at: string;
};

export type RegistryProvider = "generic" | "digitalocean" | "aws";

export type RegistryCredential = {
  id: number;
  // The stored account this authenticates as, and where its secret
  // lives. Rotating that secret is one edit there.
  credential_id: number;
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

// One DNS provider: which API is spoken, and which stored credential
// speaks it. The zones and records pages address it by this id.
export type DNSProvider = {
  id: number;
  provider: string;
  // The provider as a person calls it, served so the dashboard keeps no
  // table of its own that drifts out of step.
  provider_name: string;
  credential_id: number;
  // The credential's label, which is what a person picked it out of a
  // list by.
  label: string;
  username?: string;
  created_at: string;
  updated_at: string;
};

// One provider a DNS account can be created for, and what to ask for
// when a login is typed rather than picked. The form is built from this
// rather than from a copy of the list.
export type DNSProviderKind = {
  provider: string;
  name: string;
  // Absent where the secret is a single value — then there is no first
  // field, and asking for one would be asking for something that does
  // not exist.
  username_label?: string;
  password_label: string;
  hint: string;
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
  // What Traefik has lately complained about, whether or not the line
  // names a host. Often the only place the real reason appears.
  traefik_says?: string[];
};

// --- managed databases ---

// The engines the daemon can run. Not a hard-coded list: a version is
// permanent once a database runs it, so the daemon is the only thing
// that knows which ones it will accept — see GET /datastores/engines.
export type DatastoreEngine = {
  engine: string;
  versions: string[];
  default_version: string;
  port: number;
  has_database: boolean;
  // Whether the login is yours to choose. False for Redis, whose
  // password belongs to the ACL user `default` — which already exists
  // and cannot be renamed.
  has_user: boolean;
  // The login an empty username becomes, and the only one there is when
  // has_user is false.
  default_username: string;
  // What an attached app's variables are called: an app on a Redis gets
  // REDIS_URL, not DATABASE_URL.
  var_stem: string;
};

// What an attached app receives, by name. Values are not in it: one of
// them is the password.
//
// `app` is a full reference, project/environment/name. Full, because a
// datastore is not inside an environment and one may serve apps in
// several.
export type DatastoreAttachment = {
  app: string;
  prefix?: string;
  variables: string[];
};

// One managed database. It belongs to the instance, not to a project:
// on one host, one Postgres serving several small apps is the normal
// shape, and those apps are routinely in different projects. Its name
// is unique across the instance and is the whole of its address.
//
// No password field, deliberately. It is read from its own endpoint, by
// an admin, on purpose — see DatastoreCredentials.
export type Datastore = {
  name: string;
  description: string;
  engine: string;
  version: string;
  // The middle of the variables an attached app receives — DATABASE,
  // REDIS or MONGO. Served rather than derived here, so nothing out
  // here keeps a second copy of the engine table, and it is what says
  // whether two attachments would collide.
  var_stem: string;
  status: string;
  // Why provisioning failed, when it did.
  error?: string;
  username: string;
  database?: string;
  // Whether a container currently backs this — what decides whether
  // there is a log to read or anything to stop. The status alone cannot
  // answer it: one whose provisioning failed may have neither.
  has_container: boolean;
  // Where an app on this instance reaches it: the container's own name
  // on the shared network.
  host: string;
  port: number;
  // The host port it also answers on from outside, absent when it does
  // not — which is the default.
  exposed_port?: number;
  external_host?: string;
  attachments: DatastoreAttachment[];
  created_at: string;
};

// The statuses a database reports. "stopped" is one somebody turned
// off; "down" is one whose container stopped on its own. One is a
// decision and the other is a fault, and StatusBadge colours them
// alike — grey for both — because neither is an error to chase, but the
// word is what tells them apart.
export const DATASTORE_RUNNING = "running";
export const DATASTORE_STOPPED = "stopped";

// The response to creating one, which is the single place the password
// comes back without being asked for: whoever left the field empty is
// whoever needs to see what was generated.
export type CreatedDatastore = Datastore & { password: string };

export type DatastoreCredentials = {
  username: string;
  password: string;
  database?: string;
  internal_uri: string;
  internal_host: string;
  internal_port: number;
  external_uri?: string;
  external_host?: string;
  external_port?: number;
};

// The engines the dashboard knows how to label. One the daemon adds
// that is not here is shown by its own name rather than guessed at.
export const DATASTORE_LABELS: Record<string, string> = {
  postgres: "PostgreSQL",
  mysql: "MySQL",
  mariadb: "MariaDB",
  redis: "Redis",
  mongodb: "MongoDB",
};

export function datastoreLabel(engine: string): string {
  return DATASTORE_LABELS[engine] ?? engine;
}

// The password field of a create form comes pre-filled, so a database
// with a weak password is not something you get by not thinking about
// it. Generated here rather than left to the daemon because the person
// filling the form should be able to see it, change it, and copy it
// before anything is created.
//
// The alphabet is letters and digits only — the same one the daemon
// uses, and for the same reason: a generated password gets retyped into
// a psql prompt and pasted into config boxes, and a quote or a
// backslash in one is somebody's afternoon.
const PASSWORD_ALPHABET = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";

export function generatePassword(length = 24): string {
  const bytes = new Uint32Array(length);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (n) => PASSWORD_ALPHABET[n % PASSWORD_ALPHABET.length]).join("");
}

// datastorePath is the API path for one database. One segment: the name
// is the whole address.
export function datastorePath(name: string): string {
  return `/datastores/${name}`;
}

// --- metrics ---

// One bucket of one container's readings. `at` is the start of the
// bucket, and the API buckets server-side so a chart is the same
// density whichever window is asked for.
export type MetricSample = {
  at: string;
  // Percent of one core: 250 is two and a half cores. Deliberately not
  // capped at 100 — a container using four cores on an eight-core host
  // is a fact worth seeing.
  cpu_percent: number;
  memory_bytes: number;
  memory_limit_bytes: number;
};

export type MetricSeries = {
  window: string;
  samples: MetricSample[];
  // The ceiling the newest sample saw, which is what a memory chart is
  // drawn against. 0 when nothing has been sampled.
  memory_limit_bytes: number;
  // Whether there is a container behind this right now. False with no
  // samples means there is nothing to sample — a different sentence
  // from "nothing has been sampled yet", and the only one worth
  // showing.
  collecting: boolean;
};

// The windows the daemon offers, shortest first. Kept in step with
// metrics.Windows on the daemon, which is the side that refuses one it
// does not know.
export const METRIC_WINDOWS = ["1h", "6h", "24h"] as const;
export type MetricWindow = (typeof METRIC_WINDOWS)[number];

// Bytes as a person reads them. Binary units, because that is what a
// cgroup limit is expressed in and what every other tool shows.
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exp;
  // One decimal below 10 — 1.4 GiB says something 1 GiB does not — and
  // none above it, where the digit is noise.
  return `${value < 10 && exp > 0 ? value.toFixed(1) : Math.round(value)} ${units[exp]}`;
}

// A percentage of one core. Under 10 the fraction is the whole signal;
// above it, it is noise.
export function formatCPU(percent: number): string {
  return `${percent < 10 ? percent.toFixed(1) : Math.round(percent)}%`;
}

// --- firewall ---

// One rule as UFW prints it.
//
// `text` is what a screen shows: UFW's syntax is wider than the parsed
// fields — an interface, a rate limit — and a rule shown as less than it
// is would be a rule somebody deletes by mistake.
export type FirewallRule = {
  index: number;
  text: string;
  // `host` is traffic to the machine; `apps` is traffic forwarded to a
  // container, which is every port Cubeship publishes.
  scope: "host" | "apps";
  action?: "allow" | "deny" | "reject";
  protocol?: "tcp" | "udp";
  ports?: string;
  from?: string;
  comment?: string;
  // This rule admits SSH on a running firewall. Deleting it from here
  // is refused — it is what keeps the session you are reading this in.
  protected: boolean;
  // The IPv6 half of a rule UFW wrote twice. One decision, two lines.
  v6: boolean;
};

// A host port a container is answering on right now, which on this kind
// of machine is what is actually exposed.
export type FirewallPublishedPort = {
  port: number;
  protocol: string;
  container: string;
  allowed: boolean;
};

export type Firewall = {
  // False when the daemon is a host process rather than a container —
  // `make dev`. Then nothing else here is known.
  available: boolean;
  installed: boolean;
  enabled: boolean;
  default_incoming?: string;
  rules: FirewallRule[];
  // Whether published container ports are answerable to ufw at all.
  // False means every `apps` rule would be inert.
  docker_adopted: boolean;
  ssh_ports?: number[];
  ssh_allowed: boolean;
  // The address this request came from, so the rule form can offer
  // "just me" without sending anybody to look their own address up.
  your_ip?: string;
  published: FirewallPublishedPort[];
};
