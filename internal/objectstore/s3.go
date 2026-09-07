package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// This file is the seam between the use cases and S3 itself: what this
// module asks of a store, and the one implementation that speaks it.
//
// The interface exists so the service can be tested without a bucket
// anywhere. Everything above it — who may browse, what a folder is,
// which store a request means — is this instance's own logic and is
// worth testing on a laptop with no network.
//
// **The library is minio-go and not internal/platform/awssig**, which
// signs this daemon's other AWS calls, and the difference is not
// laziness. `awssig.Sign` takes the body as a `[]byte` because the
// signature covers its hash — right for an ECR token request and for a
// Route 53 record, and impossible for a file: it would mean holding
// every upload in the daemon's memory to sign it. Streaming means
// either `UNSIGNED-PAYLOAD` or S3's chunked signature, and which of
// those a provider accepts is exactly the sort of difference that
// arrives as a refusal with no explanation. Multipart upload, XML
// listings, path style against virtual host and per-provider error
// codes are the rest of it. This is also the reference client for the
// server a managed store runs.

// Client is the S3 API as this module needs it.
type Client interface {
	Buckets(ctx context.Context) ([]Bucket, error)
	MakeBucket(ctx context.Context, name string) error
	RemoveBucket(ctx context.Context, name string) error
	List(ctx context.Context, bucket, prefix, cursor string, limit int) (Listing, error)
	Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, Object, error)
	Remove(ctx context.Context, bucket, key string) error
	// RemoveAll deletes everything under a prefix and answers how many
	// objects went. It is what deleting a folder means, because a
	// folder is not a thing that exists to delete.
	RemoveAll(ctx context.Context, bucket, prefix string) (int, error)
}

// Connector opens a client for one store. A test supplies its own.
type Connector interface {
	Connect(ctx context.Context, s *Store) (Client, error)
}

// Errors that come back from the endpoint rather than from this
// instance. They are separate from the module's own because they mean
// something different to whoever reads them: not "you asked for the
// wrong thing" but "the store said no".
var (
	// ErrBucketNotFound is a bucket that is not there — or that this
	// login cannot see, which S3 sometimes reports the same way.
	ErrBucketNotFound = errors.New("no such bucket")
	// ErrObjectNotFound is a key that is not there.
	ErrObjectNotFound = errors.New("no such object")
	// ErrDenied is the store refusing this login. Kept apart from
	// Cubeship's own 403, which is about this instance's roles: one is
	// "you may not", the other is "the account we hold may not".
	ErrDenied = errors.New("the store refused this login")
	// ErrUnreachable is an endpoint that did not answer at all — a
	// wrong hostname, a MinIO that is not up, a firewall in between.
	ErrUnreachable = errors.New("could not reach the store")
)

// S3Connector is the real one.
type S3Connector struct{}

func (S3Connector) Connect(_ context.Context, s *Store) (Client, error) {
	lookup := minio.BucketLookupDNS
	if s.PathStyle {
		lookup = minio.BucketLookupPath
	}
	region := s.Region
	if region == "" {
		region = DefaultRegion
	}
	c, err := minio.New(s.EndpointHost(), &minio.Options{
		Creds:        credentials.NewStaticV4(s.AccessKey, s.SecretKey, ""),
		Secure:       s.Secure,
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnreachable, err)
	}
	return &s3Client{api: c}, nil
}

type s3Client struct{ api *minio.Client }

func (c *s3Client) Buckets(ctx context.Context) ([]Bucket, error) {
	found, err := c.api.ListBuckets(ctx)
	if err != nil {
		return nil, translate(err)
	}
	out := make([]Bucket, 0, len(found))
	for _, b := range found {
		out = append(out, Bucket{Name: b.Name, CreatedAt: b.CreationDate})
	}
	return out, nil
}

func (c *s3Client) MakeBucket(ctx context.Context, name string) error {
	return translate(c.api.MakeBucket(ctx, name, minio.MakeBucketOptions{}))
}

func (c *s3Client) RemoveBucket(ctx context.Context, name string) error {
	return translate(c.api.RemoveBucket(ctx, name))
}

// List is one level: the folders directly under prefix and the objects
// directly in it.
//
// The delimiter is what makes a level out of a flat namespace, and the
// page is cut here rather than by the library — its channel keeps
// fetching until the bucket is exhausted, which for a bucket with a
// million keys is a page nobody asked for. Reading `limit` entries and
// cancelling is what bounds it; the last key becomes the cursor, which
// is `start-after` on the next request.
func (c *s3Client) List(ctx context.Context, bucket, prefix, cursor string, limit int) (Listing, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	// Cancelled as soon as the page is full, which closes the channel
	// and stops the library asking for more.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := Listing{Prefix: prefix}
	last := ""
	for info := range c.api.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:     prefix,
		StartAfter: cursor,
		MaxKeys:    limit + 1,
	}) {
		if info.Err != nil {
			return Listing{}, translate(info.Err)
		}
		if len(out.Folders)+len(out.Objects) >= limit {
			// One past the page: there is more, and this is where to
			// carry on from.
			out.Cursor = last
			break
		}
		last = info.Key

		switch {
		case strings.HasSuffix(info.Key, "/") && info.Size == 0 && info.Key != prefix:
			// A common prefix. A zero-byte marker one level down looks
			// identical and is not returned twice: a key ending in the
			// delimiter rolls up into the prefix it names, so the
			// marker *is* the folder rather than a file beside it.
			out.Folders = append(out.Folders, info.Key)
		case info.Key == prefix:
			// The marker for the folder being listed. It is this
			// folder, not something in it, and showing it would put a
			// nameless zero-byte file in every folder anybody made.
		default:
			out.Objects = append(out.Objects, Object{
				Key:        info.Key,
				Size:       info.Size,
				ModifiedAt: info.LastModified,
				ETag:       strings.Trim(info.ETag, `"`),
			})
		}
	}
	return out, nil
}

func (c *s3Client) Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// size -1 is "I do not know", which the library answers by
	// multipart-uploading in chunks rather than buffering the whole
	// thing to find out — which is what an upload streamed from a
	// browser is.
	_, err := c.api.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return translate(err)
}

func (c *s3Client) Get(ctx context.Context, bucket, key string) (io.ReadCloser, Object, error) {
	obj, err := c.api.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, Object{}, translate(err)
	}
	// GetObject does not talk to the endpoint; Stat is the first call
	// that does, so a missing key surfaces here rather than as an empty
	// download the browser saves as a file.
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, Object{}, translate(err)
	}
	return obj, Object{
		Key:         info.Key,
		Size:        info.Size,
		ModifiedAt:  info.LastModified,
		ETag:        strings.Trim(info.ETag, `"`),
		ContentType: info.ContentType,
	}, nil
}

func (c *s3Client) Remove(ctx context.Context, bucket, key string) error {
	return translate(c.api.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}))
}

func (c *s3Client) RemoveAll(ctx context.Context, bucket, prefix string) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Recursive: a folder's contents are everything under it, at every
	// depth. Fed straight into RemoveObjects, which batches a thousand
	// keys per request rather than one round trip per file.
	found := c.api.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})

	// RemoveObjects drains what it is given, so the producer never
	// blocks — as long as nothing here returns early and leaves it
	// writing into a channel nobody reads. The first failure is kept
	// and the loop runs to the end for that reason.
	sending := make(chan minio.ObjectInfo)
	sent := 0
	go func() {
		defer close(sending)
		for info := range found {
			if info.Err != nil {
				continue
			}
			sent++
			sending <- info
		}
	}()

	var failed error
	for err := range c.api.RemoveObjects(ctx, bucket, sending, minio.RemoveObjectsOptions{}) {
		if failed == nil {
			failed = translate(err.Err)
		}
	}
	if failed != nil {
		return 0, failed
	}
	return sent, nil
}

// translate turns what the endpoint said into one of this module's
// errors, so a handler maps a status without knowing S3's vocabulary.
//
// Anything unrecognised is passed through rather than flattened: the
// provider's own wording is usually the only description of what went
// wrong, and losing it leaves "the store refused" as the whole story.
func translate(err error) error {
	if err == nil {
		return nil
	}
	res := minio.ToErrorResponse(err)
	switch res.Code {
	case "NoSuchBucket", "NoSuchBucketPolicy":
		return fmt.Errorf("%w: %s", ErrBucketNotFound, res.BucketName)
	case "NoSuchKey":
		return fmt.Errorf("%w: %s", ErrObjectNotFound, res.Key)
	case "BucketNotEmpty":
		return ErrBucketNotEmpty
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "UnauthorizedAccess":
		return fmt.Errorf("%w: %s", ErrDenied, res.Message)
	}
	// No S3 error code at all means it never got an S3 answer: DNS,
	// a refused connection, a timeout.
	if res.Code == "" {
		return fmt.Errorf("%w: %s", ErrUnreachable, err)
	}
	return err
}

// DefaultPageSize is how many entries a listing returns when the caller
// does not say. Enough to fill a screen twice over, and far short of
// what a bucket holds.
const DefaultPageSize = 200

// MaxPageSize bounds what a caller may ask for, because the page is
// held in memory before it is written out.
const MaxPageSize = 1000

// EndpointHost is where this store answers, as a client dials it.
//
// A managed store's is derived rather than stored: it is its own
// container's name on the shared network, so it stays correct through
// anything that changes about the network, and there is no column that
// can drift from it.
func (s *Store) EndpointHost() string {
	if s.Kind == KindManaged {
		return fmt.Sprintf("%s:%d", ContainerName(s.Slug), Port)
	}
	return s.Endpoint
}

// URL is the endpoint as a person would type it into a tool.
func (s *Store) URL() string {
	scheme := "http"
	if s.Kind == KindExternal && s.Secure {
		scheme = "https"
	}
	return scheme + "://" + s.EndpointHost()
}

// ExternalURL is where something off this host reaches a managed store,
// which exists only while it is exposed and the instance has a domain
// to be reached at. Empty otherwise — guessing an interface's address
// would be wrong on every machine behind NAT.
func (s *Store) ExternalURL(domain string) string {
	if s.Kind != KindManaged || s.ExposedPort == 0 || domain == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", domain, s.ExposedPort)
}

// bucketName is what S3 accepts: DNS-shaped, because for most providers
// it becomes part of a hostname.
var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$`)

// CheckBucketName refuses a name the endpoint would refuse, here, where
// the refusal can be a sentence rather than an XML fault.
func CheckBucketName(name string) error {
	if !bucketName.MatchString(name) {
		return ErrBadBucket
	}
	// Two dots or a dot beside a dash are refused by AWS and are a
	// certificate problem on the providers that do accept them.
	if strings.Contains(name, "..") || strings.Contains(name, ".-") || strings.Contains(name, "-.") {
		return ErrBadBucket
	}
	return nil
}

// CleanKey turns a prefix and a name typed by somebody into the key
// they meant, and refuses one that would land somewhere else.
//
// The refusal is the point. A key is a string, not a path — `../` means
// nothing to S3 and would simply create an object literally called
// that — but this instance's own listing is built out of prefixes, and
// a name that walks out of the folder it was uploaded to is a file that
// appears somewhere nobody put it. Refusing is clearer than silently
// writing what was asked for.
func CleanKey(prefix, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", ErrBadKey
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "//") {
		return "", ErrBadKey
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "." || segment == ".." {
			return "", ErrBadKey
		}
	}
	return prefix + name, nil
}

// CleanPrefix normalises a prefix from a query string: no leading
// slash, exactly one trailing one, and empty for the bucket's root.
func CleanPrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "/"))
	if prefix == "" {
		return "", nil
	}
	if strings.Contains(prefix, "//") {
		return "", ErrBadKey
	}
	for segment := range strings.SplitSeq(strings.TrimSuffix(prefix, "/"), "/") {
		if segment == "." || segment == ".." || segment == "" {
			return "", ErrBadKey
		}
	}
	return strings.TrimSuffix(prefix, "/") + "/", nil
}

// endpointHost reduces what somebody types to the host a client dials,
// and says whether they asked for TLS.
//
// Someone linking a store pastes what their provider's console shows
// them, which is a URL. Taking it apart here beats a field that refuses
// the thing everyone has on their clipboard.
func endpointHost(raw string) (host string, secure bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, ErrEndpointRequired
	}
	secure = true
	switch {
	case strings.HasPrefix(raw, "http://"):
		secure = false
		raw = strings.TrimPrefix(raw, "http://")
	case strings.HasPrefix(raw, "https://"):
		raw = strings.TrimPrefix(raw, "https://")
	}
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" || strings.ContainsAny(raw, "/?#@ ") {
		return "", false, ErrBadEndpoint
	}
	// url.Parse is what decides whether the port is a port, which is
	// the half of this nobody writes correctly by hand.
	u, parseErr := url.Parse("https://" + raw)
	if parseErr != nil || u.Host != raw {
		return "", false, ErrBadEndpoint
	}
	return strings.ToLower(raw), secure, nil
}
