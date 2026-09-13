package discovery

import (
	"bytes"
	"context"
	"fmt"
	"net/url"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Bucket is the S3-compatible bucket the site serves icons from.
type Bucket struct {
	client *minio.Client
	name   string
}

// NewBucket takes the variables an attached Cubeship bucket injects.
// endpoint is a URL, as the site's S3 client reads it too.
func NewBucket(endpoint, region, bucket, key, secret string, pathStyle bool) (*Bucket, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("S3_ENDPOINT %q is not a URL", endpoint)
	}
	lookup := minio.BucketLookupDNS
	if pathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(key, secret, ""),
		Secure:       u.Scheme == "https",
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &Bucket{client: client, name: bucket}, nil
}

func (b *Bucket) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := b.client.PutObject(ctx, b.name, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}
