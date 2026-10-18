package gitremote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

type Service struct {
	db     *database.DB
	client *http.Client
}

func NewService(db *database.DB) *Service {
	return &Service{db: db, client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (s *Service) Repo() *Repo { return NewRepository(s.db) }

func apiPath(p Provider, path string) string {
	if p == GitLab {
		return "/api/v4" + path
	}
	return "/api/v1" + path
}

func (s *Service) get(ctx context.Context, c *Connection, path string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+apiPath(c.Provider, path), nil)
	if err != nil {
		return nil, ErrRemote
	}
	if c.Provider == GitLab {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	} else {
		req.Header.Set("Authorization", "token "+c.token)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRemote, err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrToken
	case http.StatusNotFound:
		return nil, ErrRepo
	default:
		return nil, fmt.Errorf("%w: %s", ErrRemote, res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
		return nil, ErrRemote
	}
	return res.Header, nil
}

func (s *Service) Connect(ctx context.Context, caller *user.User, provider Provider, rawURL, token string) (*Connection, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return nil, err
	}
	if !provider.Valid() {
		return nil, ErrProvider
	}
	base, err := normalizeBase(rawURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrToken
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	c := &Connection{Provider: provider, BaseURL: base, UserID: caller.ID, token: token, secret: hex.EncodeToString(secret)}
	var account struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Login    string `json:"login"`
	}
	if _, err := s.get(ctx, c, "/user", &account); err != nil {
		return nil, err
	}
	c.accountID, c.Account = account.ID, account.Username
	if provider == Gitea {
		c.Account = account.Login
	}
	if c.accountID == 0 || c.Account == "" {
		return nil, ErrToken
	}
	// A token that can identify the user but not list repositories cannot
	// fulfill the connection's purpose.
	var repositories []json.RawMessage
	path := "/user/repos?limit=1"
	if provider == GitLab {
		path = "/projects?membership=true&per_page=1"
	}
	if _, err := s.get(ctx, c, path, &repositories); err != nil {
		return nil, err
	}
	if provider == Gitea {
		var organizations []json.RawMessage
		if _, err := s.get(ctx, c, "/user/orgs?limit=1", &organizations); err != nil {
			return nil, err
		}
	}
	return s.Repo().Save(ctx, c)
}

func (s *Service) List(ctx context.Context, caller *user.User) ([]*Connection, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelView, ""); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return err
	}
	return s.Repo().Delete(ctx, id)
}

func (s *Service) WebhookSecret(ctx context.Context, caller *user.User, id int64) (string, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelManage, ""); err != nil {
		return "", err
	}
	c, err := s.Repo().Get(ctx, id)
	if err != nil {
		return "", err
	}
	return c.secret, nil
}

type remoteRepository struct {
	FullName          string `json:"full_name"`
	PathWithNamespace string `json:"path_with_namespace"`
	CloneURL          string `json:"clone_url"`
	HTTPURL           string `json:"http_url_to_repo"`
	DefaultBranch     string `json:"default_branch"`
	Private           bool   `json:"private"`
	Visibility        string `json:"visibility"`
}

func (r remoteRepository) repository(c *Connection) (Repository, bool) {
	name, raw := r.FullName, r.CloneURL
	if c.Provider == GitLab {
		name, raw = r.PathWithNamespace, r.HTTPURL
	}
	path, ok := repositoryPath(c.BaseURL, raw)
	if !ok || !strings.EqualFold(path, name) {
		return Repository{}, false
	}
	private := r.Private
	if c.Provider == GitLab {
		private = r.Visibility != "public"
	}
	return Repository{FullName: name, URL: raw, Provider: c.Provider, Private: private, DefaultBranch: r.DefaultBranch}, true
}

func (s *Service) repository(ctx context.Context, c *Connection, path string) (Repository, error) {
	var r remoteRepository
	var endpoint string
	if c.Provider == GitLab {
		endpoint = "/projects/" + url.PathEscape(path)
	} else {
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return Repository{}, ErrRepo
		}
		endpoint = "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	}
	if _, err := s.get(ctx, c, endpoint, &r); err != nil {
		return Repository{}, err
	}
	repo, ok := r.repository(c)
	if !ok || !strings.EqualFold(repo.FullName, path) {
		return Repository{}, ErrRepo
	}
	return repo, nil
}

func (s *Service) page(ctx context.Context, c *Connection, path string, visit func(remoteRepository) error) error {
	for page := 1; page <= 100; page++ {
		var found []remoteRepository
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		query := path + separator + "page=" + strconv.Itoa(page)
		if c.Provider == GitLab {
			query += "&per_page=100"
		} else {
			query += "&limit=100"
		}
		head, err := s.get(ctx, c, query, &found)
		if err != nil {
			return err
		}
		for _, r := range found {
			if err := visit(r); err != nil {
				return err
			}
		}
		if c.Provider == GitLab && head.Get("X-Next-Page") == "" || c.Provider == Gitea && len(found) < 100 {
			return nil
		}
	}
	return fmt.Errorf("%w: too many repository pages", ErrRemote)
}

func (s *Service) Repositories(ctx context.Context, caller *user.User) ([]Repository, error) {
	connections, err := s.List(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := make([]Repository, 0)
	seen := map[string]bool{}
	for _, c := range connections {
		visit := func(r remoteRepository) error {
			repo, ok := r.repository(c)
			if !ok {
				return ErrRemote
			}
			if !seen[repo.URL] {
				seen[repo.URL] = true
				out = append(out, repo)
			}
			return nil
		}
		if c.Provider == GitLab {
			err = s.page(ctx, c, "/projects?membership=true", visit)
		} else {
			err = s.page(ctx, c, "/user/repos", visit)
			if err == nil {
				err = s.giteaOrganizationRepositories(ctx, c, visit)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) giteaOrganizationRepositories(ctx context.Context, c *Connection, visit func(remoteRepository) error) error {
	for page := 1; page <= 100; page++ {
		var organizations []struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		}
		_, err := s.get(ctx, c, "/user/orgs?limit=100&page="+strconv.Itoa(page), &organizations)
		if err != nil {
			return err
		}
		for _, organization := range organizations {
			name := organization.Name
			if name == "" {
				name = organization.Username
			}
			if name == "" {
				return ErrRemote
			}
			if err := s.page(ctx, c, "/orgs/"+url.PathEscape(name)+"/repos", visit); err != nil {
				return err
			}
		}
		if len(organizations) < 100 {
			return nil
		}
	}
	return fmt.Errorf("%w: too many organization pages", ErrRemote)
}

func (s *Service) find(ctx context.Context, raw string) (*Connection, Repository, error) {
	connections, err := s.Repo().List(ctx)
	if err != nil {
		return nil, Repository{}, err
	}
	var failure error
	for _, c := range connections {
		path, ok := repositoryPath(c.BaseURL, raw)
		if !ok {
			continue
		}
		repo, err := s.repository(ctx, c, path)
		if err == nil {
			return c, repo, nil
		}
		if !errors.Is(err, ErrRepo) {
			failure = err
		}
	}
	return nil, Repository{}, failure
}

func (s *Service) CredentialsForRepository(ctx context.Context, raw string) (string, string, bool, error) {
	c, _, err := s.find(ctx, raw)
	if err != nil || c == nil {
		return "", "", c != nil, err
	}
	username := "oauth2"
	if c.Provider == Gitea {
		username = c.Account
	}
	return username, c.token, true, nil
}

func (s *Service) Branches(ctx context.Context, caller *user.User, raw string) ([]Branch, error) {
	if err := user.Allow(caller, user.ResGit, user.LevelView, ""); err != nil {
		return nil, err
	}
	c, repo, err := s.find(ctx, raw)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrRepo
	}
	var endpoint string
	if c.Provider == GitLab {
		endpoint = "/projects/" + url.PathEscape(repo.FullName) + "/repository/branches"
	} else {
		parts := strings.Split(repo.FullName, "/")
		endpoint = "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/branches"
	}
	var out []Branch
	for page := 1; page <= 100; page++ {
		var found []Branch
		query := endpoint + "?page=" + strconv.Itoa(page)
		if c.Provider == GitLab {
			query += "&per_page=100"
		} else {
			query += "&limit=100"
		}
		head, err := s.get(ctx, c, query, &found)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
		if c.Provider == GitLab && head.Get("X-Next-Page") == "" || c.Provider == Gitea && len(found) < 100 {
			return out, nil
		}
	}
	return nil, ErrRemote
}
