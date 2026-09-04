package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Service holds the use cases: recording installations, minting the
// tokens a clone needs, and deciding whether a webhook is real.
type Service struct {
	db       *database.DB
	orgs     *org.Service
	settings *settings.Service

	client *http.Client
	tokens tokenCache
	now    func() time.Time
}

func NewService(db *database.DB, orgs *org.Service, cfg *settings.Service) *Service {
	return &Service{
		db: db, orgs: orgs, settings: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		now:    time.Now,
	}
}

func (s *Service) Repo() *Repo { return NewRepository(s.db) }

// Connecting an account is deciding what code this instance will build
// and run, so it takes the same role building does.
const manageRole = org.RoleAdmin

// Connect records that an organization has installed the App on a GitHub
// account. The installation id comes back from GitHub when someone
// finishes the install, and the account is which login it landed on.
func (s *Service) Connect(ctx context.Context, caller *user.User, orgSlug string, installationID int64, account string) (*Installation, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	if installationID == 0 || account == "" {
		return nil, fmt.Errorf("an installation needs an id and an account")
	}
	return s.Repo().Upsert(ctx, o.ID, installationID, account)
}

func (s *Service) List(ctx context.Context, caller *user.User, orgSlug string) ([]*Installation, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	return s.Repo().List(ctx, o.ID)
}

func (s *Service) Disconnect(ctx context.Context, caller *user.User, orgSlug string, id int64) error {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return err
	}
	if err := s.Repo().Delete(ctx, id, o.ID); errors.Is(err, database.ErrNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// TokenForRepository is what a clone authenticates with: a token scoped
// to the installation this organization holds on that repository's
// account, or nothing when there is none.
//
// Nothing is not an error. A public repository needs no token, and
// letting GitHub refuse a private one is better than refusing a clone
// that would have worked.
func (s *Service) TokenForRepository(ctx context.Context, orgID int64, repoURL string) (string, bool, error) {
	repo, ok := ParseRepositoryURL(repoURL)
	if !ok {
		return "", false, nil // not on GitHub; nothing here applies
	}

	installation, found, err := s.Repo().ForAccount(ctx, orgID, repo.Owner)
	if err != nil || !found {
		return "", false, err
	}

	token, err := s.tokenFor(ctx, installation)
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

// VerifyWebhook decides whether a delivery is really from GitHub.
//
// The signature is over the exact bytes GitHub sent, so the body has to
// be read whole before anything is decoded from it. A webhook with no
// secret configured is refused rather than trusted: an endpoint that
// starts deploys on an unauthenticated POST is a way to make this
// instance build anything.
func (s *Service) VerifyWebhook(ctx context.Context, body []byte, signature string) error {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return err
	}
	secret := values.Get(settings.GitHubWebhookSecret)
	if secret == "" {
		return ErrNoWebhookSecret
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Constant-time, and length-safe: hmac.Equal on the raw strings is
	// what stops the comparison leaking how much of it matched.
	if !hmac.Equal([]byte(strings.TrimSpace(signature)), []byte(want)) {
		return ErrBadSignature
	}
	return nil
}

// InstallURL is where someone is sent to install the App on their own
// account. Empty until the instance is registered as one.
func (s *Service) InstallURL(ctx context.Context) string {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return ""
	}
	slug := values.Get(settings.GitHubAppSlug)
	if slug == "" {
		return ""
	}
	return "https://github.com/apps/" + slug + "/installations/new"
}

// Repositories lists what this organization's installations were
// granted, across all of them.
//
// It is what the dashboard offers instead of a URL field: someone
// picking from a list cannot mistype an owner, and cannot name a
// repository this instance has no way to clone.
func (s *Service) Repositories(ctx context.Context, caller *user.User, orgSlug string) ([]RepositoryRef, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	installations, err := s.Repo().List(ctx, o.ID)
	if err != nil {
		return nil, err
	}

	var out []RepositoryRef
	seen := map[string]bool{}
	for _, installation := range installations {
		token, err := s.tokenFor(ctx, installation)
		if err != nil {
			return nil, err
		}
		repos, err := listInstallationRepositories(ctx, s.client, token)
		if err != nil {
			return nil, err
		}
		// One organization can hold installations on several accounts,
		// and a repository reachable through two of them is still one
		// repository.
		for _, r := range repos {
			if !seen[r.FullName] {
				seen[r.FullName] = true
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// Branches lists a repository's branches, for the same reason
// Repositories exists: a branch is chosen, not spelled.
func (s *Service) Branches(ctx context.Context, caller *user.User, orgSlug, fullName string) ([]Branch, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	owner, _, found := strings.Cut(fullName, "/")
	if !found || owner == "" {
		return nil, fmt.Errorf("name the repository as owner/name")
	}

	installation, found, err := s.Repo().ForAccount(ctx, o.ID, owner)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoInstallation
	}
	token, err := s.tokenFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	return listBranches(ctx, s.client, token, fullName)
}

// tokenFor mints or reuses one installation's token. It is the part of
// TokenForRepository that does not care which repository is involved.
func (s *Service) tokenFor(ctx context.Context, installation *Installation) (string, error) {
	if token, ok := s.tokens.get(installation.GitHubID, s.now()); ok {
		return token, nil
	}

	values, err := s.settings.Load(ctx)
	if err != nil {
		return "", err
	}
	if !values.HasGitHub() {
		return "", ErrNotConfigured
	}
	key, err := ParsePrivateKey(values.Get(settings.GitHubPrivateKey))
	if err != nil {
		return "", err
	}
	assertion, err := appJWT(values.Get(settings.GitHubAppID), key, s.now())
	if err != nil {
		return "", err
	}

	token, expires, err := mintToken(ctx, s.client, assertion, installation.GitHubID)
	if err != nil {
		return "", err
	}
	s.tokens.put(installation.GitHubID, token, expires)
	return token, nil
}

// RegisterFromManifest turns the code GitHub redirects back with into
// this instance's GitHub App.
//
// It writes the settings the manual path asks a person to paste, which
// is the entire reason it exists: nobody should have to copy a private
// key out of a browser to make a deploy work.
func (s *Service) RegisterFromManifest(ctx context.Context, caller *user.User, code string) (settings.Values, error) {
	// Settings are the VPS operator's, and this writes four of them.
	// Doing the exchange first would spend the code before finding out.
	if caller == nil || !caller.IsSuperAdmin {
		return nil, settings.ErrSuperAdminOnly
	}
	if code == "" {
		return nil, fmt.Errorf("no code to exchange")
	}

	app, err := convertManifest(ctx, s.client, code)
	if err != nil {
		return nil, err
	}
	return s.settings.Set(ctx, caller, map[string]string{
		settings.GitHubAppID:         strconv.FormatInt(app.ID, 10),
		settings.GitHubAppSlug:       app.Slug,
		settings.GitHubPrivateKey:    app.PEM,
		settings.GitHubWebhookSecret: app.WebhookSecret,
	})
}
