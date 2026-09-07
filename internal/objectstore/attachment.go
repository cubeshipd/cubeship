package objectstore

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"cubeship/internal/envvar"
)

// An attachment is one app pointed at one bucket, and it is the whole of
// what connects object storage to anything.
//
// The same shape a datastore's attachment has, and for the same reason:
// a store belongs to the instance, so an app reaches one by being wired
// to it rather than by being underneath it. What differs is that the
// **bucket is on the attachment**. A store holds many and an app wants
// one — `S3_BUCKET` has to have a value — which is also why one app may
// be attached to the same store twice: a bucket for uploads and one for
// backups is an ordinary shape.

// VarStem is the middle of every variable an attachment contributes.
//
// One stem, not one per provider. `S3` is what the protocol is called
// wherever it is spoken — R2, Spaces and MinIO all answer to it — and a
// variable named after the provider would have to be renamed by the app
// the day the bucket moved, which is the one thing an attachment exists
// to avoid.
const VarStem = "S3"

// Attachment is one app wired to one bucket.
type Attachment struct {
	ID       int64
	StoreID  int64
	AppID    int64
	AppRef   string
	Bucket   string
	Prefix   string
	CreateAt time.Time
}

// Vars are the variables an app attached to this store receives, for
// one bucket and under one prefix.
//
// Six of them and no URL, which is the difference from a datastore:
// there is no connection string for S3 that any client agrees on, so
// what an app needs is the five fields its SDK asks for — and the sixth
// because half the S3 clients in the world have to be told to put the
// bucket in the path and the other half guess wrong.
//
// They are **not** named `AWS_*`. That would make an app using the AWS
// SDK work with no configuration at all, and it would also mean two
// stores on one app fighting over the same six names with nothing but a
// prefix the SDK does not read. The mapping is one line in an app's own
// environment when it wants it.
func (s *Store) Vars(prefix, bucket string) envvar.Map {
	stem := prefix + VarStem
	return envvar.Map{
		stem + "_ENDPOINT":          s.URL(),
		stem + "_REGION":            s.regionOrDefault(),
		stem + "_BUCKET":            bucket,
		stem + "_ACCESS_KEY_ID":     s.AccessKey,
		stem + "_SECRET_ACCESS_KEY": s.SecretKey,
		stem + "_PATH_STYLE":        strconv.FormatBool(s.PathStyle),
	}
}

// VarNames is what Vars would write, without the values. It is what a
// listing shows: one of those values is a secret, and a screen that
// says which variables an app receives does not have to say what is in
// them.
func VarNames(prefix string) []string {
	stem := prefix + VarStem
	return []string{
		stem + "_ENDPOINT", stem + "_REGION", stem + "_BUCKET",
		stem + "_ACCESS_KEY_ID", stem + "_SECRET_ACCESS_KEY", stem + "_PATH_STYLE",
	}
}

func (s *Store) regionOrDefault() string {
	if s.Region == "" {
		return DefaultRegion
	}
	return s.Region
}

// prefixPattern is the front half of a variable name, ending in the
// separator, so prefix + "S3_ENDPOINT" is itself a legal name.
var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_$`)

// CheckPrefix accepts an empty prefix — the usual case — and otherwise
// what would still be a legal variable name once a suffix is appended.
func CheckPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if !prefixPattern.MatchString(prefix) {
		return ErrBadPrefix
	}
	return nil
}

var (
	// ErrBadPrefix is a prefix that would not be a legal environment
	// variable name once a suffix is appended to it.
	ErrBadPrefix = errors.New(`prefix must be uppercase letters, digits and underscores, ending in "_" — e.g. "BACKUPS_"`)

	// ErrPrefixTaken reports that the app already receives the same
	// variables from another attachment. Two would be one variable with
	// two values, and which one won would be a detail of map iteration.
	//
	// PrefixTakenError is what is actually returned; this is what it
	// answers errors.Is with, so the status mapping keeps working
	// without knowing about the wording.
	ErrPrefixTaken = errors.New("that app already receives those variables from another bucket")

	// ErrAlreadyAttached is the same app pointed at the same bucket
	// twice.
	ErrAlreadyAttached = errors.New("that app is already attached to this bucket")

	// ErrNotAttached is detaching something that was never attached.
	ErrNotAttached = errors.New("that app is not attached to this bucket")
)

// PrefixTakenError names the variables that collide.
//
// Which variables, not which prefix: an app may already hold two
// buckets and be colliding with exactly one of them, and "that prefix is
// taken" leaves somebody to work out which.
func PrefixTakenError(prefix string) error {
	return fmt.Errorf("%w: it already gets %s_ENDPOINT and its parts from one. Give this attachment a prefix, so the two do not name the same variables",
		ErrPrefixTaken, prefix+VarStem)
}
