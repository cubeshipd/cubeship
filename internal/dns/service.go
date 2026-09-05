package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service is every use case for DNS. The rules live here and nowhere
// else: http.go parses input and renders the answer, and that is all it
// does.
type Service struct {
	db     *database.DB
	client *http.Client
}

func NewService(db *database.DB) *Service {
	return &Service{
		db: db,
		// A provider that has not answered in this long is not going to.
		// A dashboard row is waiting on some of these calls.
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// manageRole is the bar for everything here.
//
// Not a member's job, and the reason is what a DNS credential can do:
// it moves where a name points, for every name on the account — which
// includes names that have nothing to do with Cubeship. That is closer
// to holding the account than to deploying an app.
const manageRole = user.RoleAdmin

// Create stores a credential, after checking it is one this daemon can
// act through. A provider it cannot act on is refused rather than
// stored: accepting one would let someone save a credential that can
// never resolve anything.
func (s *Service) Create(ctx context.Context, caller *user.User, in Credential) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if !in.Provider.Valid() {
		return nil, ErrProviderRequired
	}

	in.Label = NormalizeLabel(in.Label)
	if in.Label == "" {
		return nil, ErrLabelRequired
	}
	if in.Password == "" {
		return nil, ErrPasswordRequired
	}
	// Cloudflare's credential is one value; Route 53's is two, and half
	// of one is not a credential.
	if in.Provider == ProviderRoute53 && in.Username == "" {
		return nil, ErrUsernameRequired
	}
	if in.Provider == ProviderCloudflare {
		in.Username = ""
	}

	c, err := s.Repo().Create(ctx, in)
	if database.IsUniqueViolation(err) {
		return nil, ErrLabelTaken
	}
	return c, err
}

// Update rotates the credential, renames it, or both.
//
// The provider is not editable. A credential is *for* one provider —
// the calls it makes, the way it authenticates and what its secret even
// is all follow from that — so changing it in place would not be an
// edit, it would be a different credential wearing the old one's id.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, label, username, password *string) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	existing, err := s.Repo().Get(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if label != nil {
		normalized := NormalizeLabel(*label)
		if normalized == "" {
			return nil, ErrLabelRequired
		}
		label = &normalized
	}

	// The secret travels with its key id where the provider has one: a
	// new Route 53 secret against the old key id is not a credential
	// anybody chose, and it would fail in a way that reads as the
	// secret being wrong.
	if password != nil && *password == "" {
		return nil, ErrPasswordRequired
	}
	if existing.Provider == ProviderRoute53 && password != nil && (username == nil || *username == "") {
		return nil, ErrUsernameRequired
	}
	if existing.Provider == ProviderCloudflare {
		username = nil
	}

	c, err := s.Repo().Update(ctx, id, label, username, password)
	if database.IsUniqueViolation(err) {
		return nil, ErrLabelTaken
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Service) List(ctx context.Context, caller *user.User) ([]*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
		return err
	}
	err := s.Repo().Delete(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// resolve finds one of this instance's credentials.
func (s *Service) resolve(ctx context.Context, caller *user.User, id int64) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	c, err := s.Repo().Get(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return c, err
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

	if c.Provider == ProviderCloudflare {
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
	if c.Provider == ProviderCloudflare {
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
	if c.Provider == ProviderCloudflare {
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

	if c.Provider == ProviderCloudflare {
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
	if c.Provider == ProviderCloudflare {
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
