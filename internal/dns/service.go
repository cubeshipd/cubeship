package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Managing DNS is an admin's job, reading included: the list is the map
// of what this instance can reach.
const manageRole = user.RoleAdmin

// Service is every use case for DNS. The rules live here and nowhere
// else: http.go parses input and renders the answer, and that is all it
// does.
type Service struct {
	db *database.DB

	// creds is where the secrets live. A provider row says which
	// credential it writes through; this is what turns that into a
	// login, and what lets a login typed here become a stored
	// credential in the same transaction.
	creds *credential.Service

	// cfg answers whether this instance writes its own records through
	// a provider, which is what makes deleting that one a refusal
	// rather than a surprise on the domain screen.
	cfg *settings.Service

	client *http.Client
}

func NewService(db *database.DB, creds *credential.Service, cfg *settings.Service) *Service {
	return &Service{
		db:    db,
		creds: creds,
		cfg:   cfg,
		// A provider that has not answered in this long is not going to.
		// A dashboard row is waiting on some of these calls.
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// UsesCredential answers what a delete of one credential would break
// here. See credential.Dependant — this module owns these rows, so it
// is the one that can say.
func (s *Service) UsesCredential(ctx context.Context, credentialID int64) ([]credential.Use, error) {
	providers, err := s.Repo().UsingCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	uses := make([]credential.Use, 0, len(providers))
	for _, p := range providers {
		uses = append(uses, credential.Use{Kind: "DNS provider", Name: p.Name()})
	}
	return uses, nil
}

// Accounts is every provider this instance reaches.
func (s *Service) Accounts(ctx context.Context, caller *user.User) ([]*Account, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Connect adds a provider.
//
// The login comes from one of two places, and neither is privileged
// over the other: a stored credential, or one typed here — see
// NewLogin. Both at once is not a request with an obvious reading, and
// guessing which was meant is how the wrong secret gets stored.
func (s *Service) Connect(ctx context.Context, caller *user.User, in Account, login *NewLogin) (*Account, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if !in.Provider.Valid() {
		return nil, ErrUnknownProvider
	}
	switch {
	case in.CredentialID != 0 && login != nil:
		return nil, ErrTwoLogins

	case in.CredentialID != 0:
		if _, err := s.creds.Resolve(ctx, caller, in.CredentialID); err != nil {
			return nil, err
		}
		created, err := s.Repo().Create(ctx, in)
		if database.IsUniqueViolation(err) {
			return nil, ErrAccountTaken
		}
		return created, err

	case login == nil:
		return nil, ErrCredentialRequired
	}

	// A typed login is two rows, and they go in together: a secret
	// stored beside a provider that failed to be created is a secret
	// nobody asked to keep.
	label := login.Label
	if strings.TrimSpace(label) == "" {
		label = in.Provider.Name()
	}

	var created *Account
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		cred, err := s.creds.CreateWith(ctx, caller, tx, credential.Credential{
			Label: label, Username: login.Username, Password: login.Password,
		})
		if err != nil {
			return err
		}
		in.CredentialID = cred.ID
		created, err = NewRepository(tx).Create(ctx, in)
		if database.IsUniqueViolation(err) {
			return ErrAccountTaken
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Repoint moves a provider onto a different stored credential.
//
// Which API is spoken is not editable — that is what the account *is* —
// and neither is the secret: rotating one is an edit to the credential,
// in one place, and everything using it follows.
func (s *Service) Repoint(ctx context.Context, caller *user.User, id, credentialID int64) (*Account, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if _, err := s.resolve(ctx, caller, id); err != nil {
		return nil, err
	}
	if _, err := s.creds.Resolve(ctx, caller, credentialID); err != nil {
		return nil, err
	}
	updated, err := s.Repo().Update(ctx, id, credentialID)
	switch {
	case database.IsUniqueViolation(err):
		return nil, ErrAccountTaken
	case errors.Is(err, database.ErrNotFound):
		return nil, ErrNotFound
	}
	return updated, err
}

// Disconnect removes a provider, and leaves the credential alone: the
// same secret may be pulling from a registry.
func (s *Service) Disconnect(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
		return err
	}
	if _, err := s.resolve(ctx, caller, id); err != nil {
		return err
	}
	if s.cfg != nil {
		values, err := s.cfg.Load(ctx)
		if err != nil {
			return err
		}
		if values.Get(settings.DNSProviderID) == strconv.FormatInt(id, 10) {
			return ErrInstanceDNS
		}
	}
	if err := s.Repo().Delete(ctx, id); errors.Is(err, database.ErrNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// resolve finds the provider an operation is addressed to. Every
// operation below goes through it, so a caller without the role never
// reaches somebody else's API.
func (s *Service) resolve(ctx context.Context, caller *user.User, id int64) (*Account, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	a, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return a, nil
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
		err = cfPing(ctx, s.client, c.AsCredential())
	} else {
		err = r53Ping(ctx, s.client, c.AsCredential())
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
		return cfZones(ctx, s.client, c.AsCredential())
	}
	return r53Zones(ctx, s.client, c.AsCredential())
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
		return cfRecords(ctx, s.client, c.AsCredential(), zoneID)
	}
	return r53Records(ctx, s.client, c.AsCredential(), zoneID)
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
		return cfPutRecord(ctx, s.client, c.AsCredential(), zoneID, r)
	}
	return r53PutRecord(ctx, s.client, c.AsCredential(), zoneID, r)
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
		return cfDeleteRecord(ctx, s.client, c.AsCredential(), zoneID, name, kind)
	}
	return r53DeleteRecord(ctx, s.client, c.AsCredential(), zoneID, name, kind)
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
