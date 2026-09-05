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

	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Service holds the use cases: recording installations, minting the
// tokens a clone needs, and deciding whether a webhook is real.
type Service struct {
	db       *database.DB
	settings *settings.Service

	client *http.Client
	tokens tokenCache
	// states holds the nonces that tie a manifest exchange to the flow
	// that started it. See manifestStates.
	states manifestStates
	now    func() time.Time
}

func NewService(db *database.DB, cfg *settings.Service) *Service {
	return &Service{
		db: db, settings: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		now:    time.Now,
	}
}

func (s *Service) Repo() *Repo { return NewRepository(s.db) }

// Connecting an account is deciding what code this instance will build
// and run, so it takes the same role building does.
const manageRole = user.RoleAdmin

// Connect records that an organization has installed the App on a GitHub
// account. The installation id comes back from GitHub when someone
// finishes the install, and the account is which login it landed on.
func (s *Service) Connect(ctx context.Context, caller *user.User, installationID int64, code string) (*Installation, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if installationID == 0 {
		return nil, fmt.Errorf("an installation needs an id")
	}
	if code == "" {
		return nil, ErrNoProof
	}

	values, err := s.settings.Load(ctx)
	if err != nil {
		return nil, err
	}
	clientID := values.Get(settings.GitHubClientID)
	clientSecret := values.Get(settings.GitHubClientSecret)
	if clientID == "" || clientSecret == "" {
		return nil, ErrNoOAuth
	}

	// The code proves who is asking. Everything else about this request
	// is a number the caller chose.
	userToken, err := exchangeUserCode(ctx, s.client, clientID, clientSecret, code)
	if err != nil {
		return nil, err
	}
	reachable, err := listUserInstallations(ctx, s.client, userToken)
	if err != nil {
		return nil, err
	}

	// GitHub's answer is the account name too. Taking it from the caller
	// would let one be stored that does not match the installation, and
	// the account is what every repository lookup matches against.
	for _, i := range reachable {
		if i.ID == installationID {
			return s.Repo().Upsert(ctx, installationID, i.Account.Login)
		}
	}
	return nil, ErrNotYours
}

func (s *Service) List(ctx context.Context, caller *user.User) ([]*Installation, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

func (s *Service) Disconnect(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
		return err
	}
	if err := s.Repo().Delete(ctx, id); errors.Is(err, database.ErrNotFound) {
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
func (s *Service) TokenForRepository(ctx context.Context, repoURL string) (string, bool, error) {
	repo, ok := ParseRepositoryURL(repoURL)
	if !ok {
		return "", false, nil // not on GitHub; nothing here applies
	}

	installation, found, err := s.Repo().ForAccount(ctx, repo.Owner)
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
func (s *Service) Repositories(ctx context.Context, caller *user.User) ([]RepositoryRef, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	installations, err := s.Repo().List(ctx)
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
func (s *Service) Branches(ctx context.Context, caller *user.User, fullName string) ([]Branch, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	owner, _, found := strings.Cut(fullName, "/")
	if !found || owner == "" {
		return nil, fmt.Errorf("name the repository as owner/name")
	}

	installation, found, err := s.Repo().ForAccount(ctx, owner)
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

// NewManifestState mints the nonce a registration carries to GitHub and
// back. It is the first half of RegisterFromManifest's check — see
// manifestStates for what it is for.
//
// replace says the caller knows this instance already has an App and
// means to replace it. It is bound into the nonce rather than taken from
// the request that comes back, because that request arrives from a
// redirect somebody else may have written.
func (s *Service) NewManifestState(ctx context.Context, caller *user.User, replace bool) (string, error) {
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return "", err
	}
	return s.states.issue(caller.ID, replace, s.now())
}

// RegisterFromManifest turns the code GitHub redirects back with into
// this instance's GitHub App.
//
// It writes the settings the manual path asks a person to paste, which
// is the entire reason it exists: nobody should have to copy a private
// key out of a browser to make a deploy work.
//
// state is the nonce this daemon issued to this caller when the flow
// started, and it is required. The role is not enough on its own: the
// code arrives in a query string, GitHub's conversion endpoint is
// unauthenticated, and the browser sends the session cookie whoever
// wrote the link. Without the nonce, a link to /github/app-created sent
// to a signed-in admin would quietly make this instance somebody else's
// App — their webhook secret, their private key, their installation
// tokens over every repository the admin then grants.
func (s *Service) RegisterFromManifest(ctx context.Context, caller *user.User, code, state string) (settings.Values, error) {
	// Settings are the VPS operator's, and this writes six of them.
	// Doing the exchange first would spend the code before finding out.
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return nil, err
	}
	if code == "" {
		return nil, fmt.Errorf("no code to exchange")
	}
	started, ok := s.states.consume(state, caller.ID, s.now())
	if !ok {
		return nil, ErrNotStartedHere
	}

	// Replacing the App an instance already has breaks every
	// installation on it, so it is a thing to mean rather than a thing
	// to arrive at. The answer comes from the nonce, which was issued
	// before GitHub was ever involved.
	if !started.replace {
		current, err := s.settings.Load(ctx)
		if err != nil {
			return nil, err
		}
		if current.Get(settings.GitHubAppID) != "" {
			return nil, ErrAlreadyRegistered
		}
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
		settings.GitHubClientID:      app.ClientID,
		settings.GitHubClientSecret:  app.ClientSecret,
	})
}
