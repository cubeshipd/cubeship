// Package objectstore owns the object storage this instance can reach:
// the buckets it holds, the files inside them, and the two ways a store
// gets here.
//
// **Two ways, one module.** A store is either a MinIO this instance
// runs — a container beside the databases, on the same network, with
// its data in the same directory — or an endpoint somewhere else that
// this instance merely holds the keys to. They are the same thing from
// every direction that matters: an S3 endpoint, a login, and buckets
// inside it. Everything above the connection is one code path, and
// `Kind` is the only branch.
//
// That split is also the honest answer to what object storage is *for*
// here. The convention is backups, and a backup of this machine kept on
// this machine is not a backup — so the common case is linking a bucket
// somewhere else. But it is not the only case: a MinIO on the box is
// where an app's uploads go without an account anywhere, where a
// database dump lands before something ships it off, and where you
// develop against S3 without paying for S3. Which of those you are
// doing is not this module's to decide, so it runs one and links the
// other.
//
// **It stores no files and proxies every byte.** There is no cache and
// no copy: a download is read from the store and written to the
// response, an upload the other way. A presigned URL would be faster
// and would be wrong here — a managed store's endpoint is a container
// name that resolves on the Docker network and nowhere else, so the
// link would be one only the daemon could follow.
package objectstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cubeship/internal/limits"
)

// Kind says where a store is.
type Kind string

const (
	// KindManaged is a MinIO container this instance runs.
	KindManaged Kind = "managed"
	// KindExternal is an S3 endpoint somewhere else — AWS, R2, Spaces,
	// a MinIO on another machine.
	KindExternal Kind = "external"
)

func (k Kind) Valid() bool { return k == KindManaged || k == KindExternal }

// Provider decides how an endpoint is spelled and how a bucket is
// addressed in it.
//
// It is not a list of who is allowed. `ProviderGeneric` takes an
// endpoint and reaches anything that speaks S3; the named ones exist
// because their endpoint is a template with one variable in it, and
// asking for a region beats asking somebody to retype
// `s3.eu-central-1.amazonaws.com` and get one character wrong.
type Provider string

const (
	// ProviderMinIO is what this instance runs. Only a managed store
	// has it, and it is not offered when linking one — a MinIO
	// elsewhere is reached as a generic endpoint, because from here
	// that is all it is.
	ProviderMinIO Provider = "minio"

	// ProviderAWS is S3 itself. The endpoint follows the region.
	ProviderAWS Provider = "aws"

	// ProviderCloudflare is R2. Its endpoint carries the account id,
	// and its region is the literal string "auto" — R2 has one.
	ProviderCloudflare Provider = "cloudflare"

	// ProviderDigitalOcean is Spaces. Endpoint follows the region, like
	// AWS, with a different suffix.
	ProviderDigitalOcean Provider = "digitalocean"

	// ProviderGeneric is an endpoint typed in full: Backblaze, Wasabi,
	// Scaleway, Hetzner, a MinIO on another box.
	ProviderGeneric Provider = "generic"
)

// Providers is what may be linked, in the order a form should offer
// them. MinIO is absent on purpose: it is what `POST /objectstores`
// creates, not something linked.
func Providers() []Provider {
	return []Provider{ProviderAWS, ProviderCloudflare, ProviderDigitalOcean, ProviderGeneric}
}

func (p Provider) Valid() bool {
	switch p {
	case ProviderMinIO, ProviderAWS, ProviderCloudflare, ProviderDigitalOcean, ProviderGeneric:
		return true
	}
	return false
}

// Asks is what a provider needs from whoever links it, beyond the
// login. Exactly one field each, which is why this is a string rather
// than a set of booleans.
type Asks string

const (
	// AsksRegion is AWS and DigitalOcean: the region is the variable
	// part of the endpoint.
	AsksRegion Asks = "region"
	// AsksAccount is Cloudflare: R2's endpoint carries the account id.
	AsksAccount Asks = "account"
	// AsksEndpoint is everything else: the address itself.
	AsksEndpoint Asks = "endpoint"
	// AsksNothing is a managed store, which is not asked anything.
	AsksNothing Asks = ""
)

// Asks says what a form should put in front of somebody linking this
// provider.
func (p Provider) Asks() Asks {
	switch p {
	case ProviderAWS, ProviderDigitalOcean:
		return AsksRegion
	case ProviderCloudflare:
		return AsksAccount
	case ProviderGeneric:
		return AsksEndpoint
	}
	return AsksNothing
}

// Label is the provider's name as a person writes it.
func (p Provider) Label() string {
	switch p {
	case ProviderMinIO:
		return "MinIO"
	case ProviderAWS:
		return "Amazon S3"
	case ProviderCloudflare:
		return "Cloudflare R2"
	case ProviderDigitalOcean:
		return "DigitalOcean Spaces"
	case ProviderGeneric:
		return "S3-compatible"
	}
	return string(p)
}

// Store is one place objects live.
type Store struct {
	ID int64

	// Slug names it on the whole instance and is permanent. For a
	// managed store it is the container's name, which is the host an
	// app on this instance connects to — renaming it would break every
	// endpoint already handed out.
	Slug string
	// Description is what this storage is for. With nothing above a
	// store to say where it belongs, this is the only place that can.
	Description string

	Kind     Kind
	Provider Provider

	// Endpoint is host and optional port, no scheme. Empty on a managed
	// store, whose endpoint is derived from its own container name —
	// storing it would be storing a value that has to be rewritten if
	// anything about the network changes.
	Endpoint string
	// Region is what the signature is computed for. Every S3 signature
	// carries one whether the provider has regions or not, which is why
	// a store with none still gets DefaultRegion.
	Region string
	// Secure is https rather than http.
	Secure bool
	// PathStyle puts the bucket in the path instead of the hostname.
	// AWS wants the hostname; most compatible endpoints want the path,
	// and a wrong answer is a DNS name that does not resolve.
	PathStyle bool

	// Bucket is the one bucket this store *is*, for a credential that
	// reaches exactly one and cannot list them — an R2 token scoped to
	// a bucket, an IAM policy without ListAllMyBuckets. Empty is the
	// normal case: ask the endpoint what is there.
	Bucket string

	// CredentialID is the account an external store authenticates as,
	// and 0 for a managed one. The secret is not here — see
	// internal/credential: one AWS key reaches S3, ECR and Route 53,
	// and it is stored once and pointed at three times.
	CredentialID int64
	// AccessKey and SecretKey are filled on read: from the credential
	// for an external store, from this row for a managed one. Nothing
	// above this has to know which.
	AccessKey string
	SecretKey string

	// Version is the MinIO release a managed store runs. Permanent, for
	// the reason a datastore's is: the data directory belongs to the
	// server that wrote it.
	Version string
	// ExposedPort is the host port a managed store also answers on, or
	// 0 for reachable only by its neighbours — the default.
	ExposedPort int
	// Limits is what a managed store's container may take from the
	// machine. Always zero on a linked store: that is somebody else's
	// server, so there is no container here to cap and never will be.
	Limits limits.Limits

	ContainerID string
	Status      string
	// Error is why provisioning failed, when it did.
	Error string

	CreatedAt time.Time
	UpdatedAt time.Time

	// Attachments are the apps that receive this store's connection
	// variables. Loaded with the store rather than asked for
	// separately: with nothing above it, what a store is wired to is
	// the whole of where it sits in the instance.
	Attachments []Attachment
}

// Statuses a store can be in.
//
// The first four are a managed store's and mean exactly what they mean
// for a datastore. The last is an external one's, and it is not a
// pretence at a health check: this instance holds a login, and whether
// the endpoint answers is found out by opening it, not by a column that
// would be stale the moment it was written.
const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusStopped      = "stopped"
	StatusDown         = "down"
	StatusFailed       = "failed"
	StatusLinked       = "linked"
)

// Network is the Docker network a managed store joins — the same one
// apps and databases are on, which is the whole of how an app reaches
// one.
const Network = "cubeship"

// containerPrefix is what a managed store's container is named under.
// Its own, like the databases' `cubeship-db-`, so a store called `api`
// may sit beside a database called `api` and an app called `api`.
const containerPrefix = "cubeship-s3-"

// ContainerName is what slug's container is called, and so the host an
// app on this instance connects to.
func ContainerName(slug string) string { return containerPrefix + slug }

// Port is what MinIO listens on inside its container. The console's
// 9001 is not published or run: this dashboard is the console.
const Port = 9000

// DefaultRegion is what a managed store signs for. MinIO accepts any
// region and this is the one every S3 client defaults to, so a tool
// pointed at a managed store with no configuration at all still works.
const DefaultRegion = "us-east-1"

// PortRangeStart and PortRangeEnd bound the host ports handed out when
// somebody exposes a store without naming one.
//
// Deliberately not the datastores' 15000-15999. Both would otherwise
// pick from one range while each checked only its own table, and the
// collision would appear as a container that will not bind, minutes
// later, for no reason visible on either screen.
const (
	PortRangeStart = 16000
	PortRangeEnd   = 16999
)

// reservedSlugs are names this module's own API needs as path segments
// beside a slug of the same shape. Only `providers`: Go's mux prefers
// the literal, so a store called that would be a resource nothing could
// open. Local rather than global, like the datastores' `engines`.
var reservedSlugs = map[string]bool{"providers": true}

var (
	ErrNotFound      = errors.New("object store not found")
	ErrAlreadyExists = errors.New("an object store with that name already exists on this instance")
	ErrReservedSlug  = errors.New(`"providers" is reserved: it is where the API lists the providers this release links`)

	ErrUnknownKind     = errors.New(`kind must be "managed" or "external"`)
	ErrUnknownProvider = errors.New("unknown object storage provider")
	ErrUnknownVersion  = errors.New("unknown MinIO version for this release")

	// ErrTwoLogins is a request naming a stored account *and* typing
	// one. Both is not a request with an obvious reading, and guessing
	// which was meant is how the wrong secret gets stored.
	ErrTwoLogins = errors.New("pick a stored account or type keys, not both")
	// ErrCredentialRequired is an external store with no way in.
	ErrCredentialRequired = errors.New("no login: pick the account this store authenticates as, or type an access key and secret")

	ErrRegionRequired   = errors.New("a region is required: this provider's endpoint is derived from it")
	ErrAccountRequired  = errors.New("the Cloudflare account id is required — it is what R2's endpoint is named after")
	ErrEndpointRequired = errors.New("an endpoint is required, e.g. s3.eu-central-1.wasabisys.com")
	ErrBadEndpoint      = errors.New("that endpoint is not a host: give a hostname, optionally with a port")

	// ErrManagedFixed refuses editing what a managed store is. Its
	// endpoint is derived and its keys were minted here; there is
	// nothing about the connection to correct.
	ErrManagedFixed = errors.New("a managed store's connection is this instance's own: there is nothing to re-point")
	// ErrExternalHasNoContainer refuses a container operation on a
	// store this instance does not run.
	ErrExternalHasNoContainer = errors.New("this store runs somewhere else, so there is no container here to start, stop or expose")
	ErrNotRunning             = errors.New("this store has no container running")

	ErrBadPort     = errors.New("port must be between 1024 and 65535")
	ErrPortTaken   = errors.New("another object store is already exposed on that port")
	ErrNoPortsLeft = fmt.Errorf("no free port left in the range %d-%d; name one explicitly", PortRangeStart, PortRangeEnd)

	// ErrBadBucket is a bucket name S3 itself would refuse. Checked
	// here so the refusal is a sentence rather than an XML fault
	// somebody has to read a provider's documentation to decode.
	ErrBadBucket = errors.New("a bucket name is 3 to 63 characters of lowercase letters, digits, dots and dashes, starting and ending with a letter or digit")
	// ErrBadKey is a key that would escape the prefix it is being
	// written under, or one that is empty.
	ErrBadKey = errors.New("invalid object key")
	// ErrSingleBucket refuses reaching past the one bucket a store was
	// pinned to.
	ErrSingleBucket = errors.New("this store is one bucket, and that is not it")
	// ErrBucketNotEmpty refuses deleting a bucket with objects in it,
	// which is S3's own refusal repeated where somebody can act on it.
	ErrBucketNotEmpty = errors.New("this bucket still has objects in it; empty it first")
)

// Bucket is one bucket, as a listing reports it.
type Bucket struct {
	Name      string
	CreatedAt time.Time
}

// Object is one file.
type Object struct {
	// Key is the whole key, from the root of the bucket. The listing
	// carries it in full rather than relative to the prefix, because a
	// key is what every other call takes and rebuilding it from two
	// halves is how the halves come apart.
	Key         string
	Size        int64
	ModifiedAt  time.Time
	ETag        string
	ContentType string
}

// Name is the last segment of the key — what a file is called inside
// the folder it appears in.
func (o Object) Name() string { return path(o.Key) }

// Listing is one level of one bucket: the folders directly under a
// prefix, and the objects directly in it.
//
// **Folders are not a thing S3 has.** What a listing returns is keys and
// common prefixes, and a folder is the second of those — an artefact of
// asking for a delimiter. That is why creating one writes a zero-byte
// object at `prefix/` (the convention every console uses) and why
// deleting one removes everything under it: there is nothing else to
// delete.
type Listing struct {
	Prefix  string
	Folders []string
	Objects []Object
	// Cursor continues the listing when the endpoint had more to say
	// than one page. Empty when there is no more.
	Cursor string
}

// FolderMarker is the suffix of the zero-byte object that makes an
// empty folder appear in a listing.
const FolderMarker = "/"

// path is the last segment of a key, with any trailing slash removed —
// so both `a/b/c.txt` and `a/b/` answer with their own last name.
func path(key string) string {
	trimmed := strings.TrimSuffix(key, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// FolderName is the last segment of a common prefix — what the folder
// is called inside the folder it appears in.
func FolderName(prefix string) string { return path(prefix) }

// Limits is what a managed store's container may take. An alias, so
// every module that caps a container means exactly the same thing by it.
type Limits = limits.Limits

// ErrInvalidLimits is a ceiling this instance will not set.
var ErrInvalidLimits = limits.ErrInvalid

// ErrLinkedHasNoContainer refuses a limit on a store this instance does
// not run.
//
// Not stored and ignored: a number that does nothing is worse than a
// refusal, because the screen would then show a ceiling on a server this
// instance has no say over. Same shape as ErrManagedFixed, from the
// other side — each kind refuses the setting that belongs to the other.
var ErrLinkedHasNoContainer = errors.New("a linked store runs on somebody else's server, so there is no container here to cap")
