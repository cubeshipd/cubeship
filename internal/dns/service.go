package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/user"
)

// Service is every use case for DNS. The rules live here and nowhere
// else: http.go parses input and renders the answer, and that is all it
// does.
type Service struct {
	// creds is where the accounts live. This module has none of its
	// own — see the package comment.
	creds  *credential.Service
	client *http.Client
}

func NewService(creds *credential.Service) *Service {
	return &Service{
		creds: creds,
		// A provider that has not answered in this long is not going to.
		// A dashboard row is waiting on some of these calls.
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// resolve finds the account an operation is addressed to, and refuses
// one that cannot do DNS.
//
// Both questions belong to the credential module: it owns the row, and
// what an account can be used for follows from its provider, which only
// that module knows. This package asks once, here, and every operation
// below gets an account it can act through.
func (s *Service) resolve(ctx context.Context, caller *user.User, id int64) (*Credential, error) {
	return s.creds.Resolve(ctx, caller, id, credential.CapabilityDNS)
}

// Probe asks a provider whether this credential still works.
//
// A live call rather than something recorded at creation, because the
// interesting case is a credential that used to work: a Cloudflare token
// revoked, an access key deleted in IAM. Neither tells Cubeship
// anything — the first sign would be a record edit failing — and the
// point of asking now is to find out before that.
func (s *Service) Probe(ctx context.Context, caller *user.User, id int64) (*Status, error) {
	c, err := s.resolve(ctx, caller, id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	if c.Provider == credential.ProviderCloudflare {
		err = cfPing(ctx, s.client, c)
	} else {
		err = r53Ping(ctx, s.client, c)
	}
	if err == nil {
		return &Status{State: StateAvailable}, nil
	}
	if errors.Is(err, errUnauthorized) {
		return &Status{State: StateUnauthorized, Detail: err.Error()}, nil
	}
	return &Status{State: StateUnreachable, Detail: err.Error()}, nil
}

// probeTimeout bounds a probe. A dashboard row is waiting on it, and a
// provider that has not answered in this long is not available whatever
// it would eventually have said.
const probeTimeout = 10 * time.Second

// Zones lists the domains a credential can reach.
func (s *Service) Zones(ctx context.Context, caller *user.User, id int64) ([]Zone, error) {
	c, err := s.resolve(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if c.Provider == credential.ProviderCloudflare {
		return cfZones(ctx, s.client, c)
	}
	return r53Zones(ctx, s.client, c)
}

// Records lists one zone's entries.
func (s *Service) Records(ctx context.Context, caller *user.User, id int64, zoneID string) ([]Record, error) {
	c, err := s.resolve(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if zoneID == "" {
		return nil, fmt.Errorf("name the zone to list")
	}
	if c.Provider == credential.ProviderCloudflare {
		return cfRecords(ctx, s.client, c, zoneID)
	}
	return r53Records(ctx, s.client, c, zoneID)
}

// PutRecord writes a record, creating it or replacing what is at that
// name and type.
//
// One operation rather than a create and an update, because that is
// what one of the two providers actually offers: Route 53's UPSERT
// creates or replaces the set whole, and a create/update split would be
// two names for one call there and a race between them at the other.
func (s *Service) PutRecord(ctx context.Context, caller *user.User, id int64, zoneID string, r Record) error {
	c, err := s.resolve(ctx, caller, id)
	if err != nil {
		return err
	}
	if zoneID == "" {
		return fmt.Errorf("name the zone to write to")
	}
	if !ValidRecordType(r.Type) {
		return ErrRecordTypeUnknown
	}

	r.Name = NormalizeName(r.Name)
	if r.Name == "" {
		return fmt.Errorf("name the record")
	}
	r.Values = cleanValues(r.Values)
	if len(r.Values) == 0 {
		return fmt.Errorf("a record with no value points at nothing")
	}
	if r.TTL <= 0 {
		r.TTL = DefaultTTL
	}

	if c.Provider == credential.ProviderCloudflare {
		return cfPutRecord(ctx, s.client, c, zoneID, r)
	}
	return r53PutRecord(ctx, s.client, c, zoneID, r)
}

// DeleteRecord removes everything at one name and type.
func (s *Service) DeleteRecord(ctx context.Context, caller *user.User, id int64, zoneID, name, kind string) error {
	c, err := s.resolve(ctx, caller, id)
	if err != nil {
		return err
	}
	if zoneID == "" || name == "" || kind == "" {
		return fmt.Errorf("name the zone, the record and its type")
	}

	name = NormalizeName(name)
	if c.Provider == credential.ProviderCloudflare {
		return cfDeleteRecord(ctx, s.client, c, zoneID, name, kind)
	}
	return r53DeleteRecord(ctx, s.client, c, zoneID, name, kind)
}

// cleanValues drops what someone did not mean to send: blank lines out
// of a textarea, and the whitespace around a value pasted from
// somewhere else.
func cleanValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
