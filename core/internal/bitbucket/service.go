package bitbucket

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

const StateTTL = 10 * time.Minute
const apiURL = "https://api.bitbucket.org/2.0"
const tokenURL = "https://bitbucket.org/site/oauth2/access_token"

type Service struct {
	db       *database.DB
	settings *settings.Service
	client   *http.Client
	now      func() time.Time
}

func NewService(db *database.DB, cfg *settings.Service) *Service {
	return &Service{db: db, settings: cfg, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}
func (s *Service) Repo() *Repo { return NewRepository(s.db) }
func stateHash(state string) string {
	h := sha256.Sum256([]byte(state))
	return hex.EncodeToString(h[:])
}
func (s *Service) consumer(ctx context.Context) (string, string, error) {
	v, err := s.settings.Load(ctx)
	if err != nil {
		return "", "", err
	}
	id, secret := v.Get(settings.BitbucketClientID), v.Get(settings.BitbucketClientSecret)
	if id == "" || secret == "" {
		return "", "", ErrNotConfigured
	}
	return id, secret, nil
}
func (s *Service) Start(ctx context.Context, caller *user.User) (string, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return "", err
	}
	id, _, err := s.consumer(ctx)
	if err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b)
	if err = s.Repo().IssueState(ctx, stateHash(state), caller.ID, id, s.now().Add(StateTTL), s.now()); err != nil {
		return "", err
	}
	return "https://bitbucket.org/site/oauth2/authorize?" + url.Values{"client_id": {id}, "response_type": {"code"}, "state": {state}}.Encode(), nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *Service) exchange(ctx context.Context, id, secret string, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, ErrProvider
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, ErrProvider
	}
	defer res.Body.Close()
	if res.StatusCode == 400 || res.StatusCode == 401 {
		return tokenResponse{}, ErrReconnect
	}
	if res.StatusCode != 200 {
		return tokenResponse{}, ErrProvider
	}
	var token tokenResponse
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&token) != nil || token.AccessToken == "" || token.RefreshToken == "" || !strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > 86400 {
		return tokenResponse{}, ErrProvider
	}
	return token, nil
}
func (s *Service) Connect(ctx context.Context, caller *user.User, code, state string) (*Connection, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return nil, err
	}
	if code == "" || len(state) != 64 {
		return nil, ErrState
	}
	id, secret, err := s.consumer(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.Repo().ConsumeState(ctx, stateHash(state), caller.ID, id, s.now()); err != nil {
		return nil, err
	}
	token, err := s.exchange(ctx, id, secret, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
	if err != nil {
		return nil, err
	}
	var account struct {
		UUID        string `json:"uuid"`
		DisplayName string `json:"display_name"`
	}
	if err = s.get(ctx, token.AccessToken, apiURL+"/user", &account); err != nil {
		return nil, err
	}
	if account.UUID == "" || account.DisplayName == "" {
		return nil, ErrProvider
	}
	return s.Repo().Save(ctx, &Connection{UserID: caller.ID, UUID: account.UUID, Account: account.DisplayName, consumerID: id, accessToken: token.AccessToken, refreshToken: token.RefreshToken, expiresAt: s.now().Add(time.Duration(token.ExpiresIn) * time.Second)})
}
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Connection, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelView, ""); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}
func (s *Service) Disconnect(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return err
	}
	return s.Repo().Delete(ctx, id)
}

// The row lock covers the refresh request and rotation together, including other daemon processes.
func (s *Service) tokenFor(ctx context.Context, id int64) (string, error) {
	consumer, secret, err := s.consumer(ctx)
	if err != nil {
		return "", err
	}
	var token string
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		c, err := repo.Lock(ctx, id)
		if err != nil {
			return err
		}
		if c.consumerID != consumer {
			return ErrReconnect
		}
		if c.expiresAt.After(s.now().Add(time.Minute)) {
			token = c.accessToken
			return nil
		}
		next, err := s.exchange(ctx, consumer, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.refreshToken}})
		if err != nil {
			return err
		}
		c.accessToken = next.AccessToken
		c.refreshToken = next.RefreshToken
		c.expiresAt = s.now().Add(time.Duration(next.ExpiresIn) * time.Second)
		if err = repo.UpdateTokens(ctx, c); err != nil {
			return err
		}
		token = c.accessToken
		return nil
	})
	return token, err
}
func safeAPIURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "api.bitbucket.org" && u.User == nil && u.Fragment == "" && strings.HasPrefix(u.Path, "/2.0/")
}
func (s *Service) get(ctx context.Context, token, raw string, out any) error {
	if !safeAPIURL(raw) {
		return ErrProvider
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ErrProvider
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := s.client.Do(req)
	if err != nil {
		return ErrProvider
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 401:
		return ErrReconnect
	case 403, 404:
		return ErrRepository
	case 200:
	default:
		return ErrProvider
	}
	if json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out) != nil {
		return ErrProvider
	}
	return nil
}

type remoteRepository struct {
	FullName   string `json:"full_name"`
	Private    bool   `json:"is_private"`
	MainBranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	UUID string `json:"uuid"`
}

func (s *Service) pages(ctx context.Context, token, raw string, visit func(json.RawMessage) error) error {
	seen := map[string]bool{}
	for raw != "" {
		if seen[raw] || len(seen) >= 1000 {
			return ErrProvider
		}
		seen[raw] = true
		var page struct {
			Next   string            `json:"next"`
			Values []json.RawMessage `json:"values"`
		}
		if err := s.get(ctx, token, raw, &page); err != nil {
			return err
		}
		for _, value := range page.Values {
			if err := visit(value); err != nil {
				return err
			}
		}
		raw = page.Next
	}
	return nil
}
func (s *Service) Repositories(ctx context.Context, caller *user.User) ([]RepositoryRef, error) {
	connections, err := s.List(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := make([]RepositoryRef, 0)
	seen := map[string]bool{}
	for _, c := range connections {
		token, err := s.tokenFor(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		err = s.pages(ctx, token, apiURL+"/user/permissions/repositories?pagelen=100", func(raw json.RawMessage) error {
			var entry struct {
				Repository remoteRepository `json:"repository"`
			}
			if json.Unmarshal(raw, &entry) != nil {
				return ErrProvider
			}
			r := entry.Repository
			if _, ok := ParseRepositoryURL("https://bitbucket.org/" + r.FullName); !ok {
				return ErrProvider
			}
			if !seen[r.FullName] {
				seen[r.FullName] = true
				out = append(out, RepositoryRef{r.FullName, r.Private, r.MainBranch.Name})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Service) repositoryToken(ctx context.Context, r Repository) (string, *remoteRepository, error) {
	connections, err := s.Repo().List(ctx)
	if err != nil {
		return "", nil, err
	}
	var failure error
	for _, c := range connections {
		token, err := s.tokenFor(ctx, c.ID)
		if err != nil {
			failure = err
			continue
		}
		var remote remoteRepository
		err = s.get(ctx, token, apiURL+"/repositories/"+r.FullName(), &remote)
		if err != nil {
			if !errors.Is(err, ErrRepository) {
				failure = err
			}
			continue
		}
		if !strings.EqualFold(remote.FullName, r.FullName()) || remote.UUID == "" {
			return "", nil, ErrProvider
		}
		return token, &remote, nil
	}
	return "", nil, failure
}
func (s *Service) TokenForRepository(ctx context.Context, raw string) (string, bool, error) {
	r, ok := ParseRepositoryURL(raw)
	if !ok {
		return "", false, nil
	}
	token, _, err := s.repositoryToken(ctx, r)
	return token, token != "", err
}
func (s *Service) Branches(ctx context.Context, caller *user.User, fullName string) ([]Branch, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelView, ""); err != nil {
		return nil, err
	}
	r, ok := ParseRepositoryURL("https://bitbucket.org/" + fullName)
	if !ok {
		return nil, ErrRepository
	}
	token, _, err := s.repositoryToken(ctx, r)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, ErrRepository
	}
	out := make([]Branch, 0)
	err = s.pages(ctx, token, apiURL+"/repositories/"+r.FullName()+"/refs/branches?pagelen=100", func(raw json.RawMessage) error {
		var b Branch
		if json.Unmarshal(raw, &b) != nil || b.Name == "" {
			return ErrProvider
		}
		out = append(out, b)
		return nil
	})
	return out, err
}

// PushBranches verifies both the delivery and a live grant before handing anything to the deployer.
func (s *Service) PushBranches(ctx context.Context, body []byte, signature, event string) (string, []string, error) {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return "", nil, err
	}
	if err = VerifyWebhook(body, values.Get(settings.BitbucketWebhookSecret), signature); err != nil {
		return "", nil, err
	}
	if event != "repo:push" {
		return "", nil, nil
	}
	var payload struct {
		Repository remoteRepository `json:"repository"`
		Push       struct {
			Changes []struct {
				New *struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"new"`
			} `json:"changes"`
		} `json:"push"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", nil, ErrRepository
	}
	r, ok := ParseRepositoryURL("https://bitbucket.org/" + payload.Repository.FullName)
	if !ok {
		return "", nil, ErrRepository
	}
	token, remote, err := s.repositoryToken(ctx, r)
	if err != nil {
		return "", nil, err
	}
	if token == "" || remote.UUID != payload.Repository.UUID {
		return "", nil, ErrRepository
	}
	branches := make([]string, 0)
	seen := map[string]bool{}
	for _, change := range payload.Push.Changes {
		if change.New != nil && change.New.Type == "branch" && change.New.Name != "" && !seen[change.New.Name] {
			seen[change.New.Name] = true
			branches = append(branches, change.New.Name)
		}
	}
	return "bitbucket.org/" + r.FullName(), branches, nil
}
