package extregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// GHCR does not provide a useful _catalog: it returns public packages
// unrelated to the login, while omitting packages that login can pull.
// GitHub's package API is scoped to the authenticated owner and the
// organizations they belong to, which is the set the registry screen
// should show.
func listGHCRRepositories(ctx context.Context, client *http.Client, c *Credential) ([]Repo, error) {
	username, token := c.Login()
	if token == "" || username == "" {
		return nil, errUnauthorized
	}

	seen := make(map[string]struct{})
	add := func(packages []ghcrPackage, fallbackOwner string) {
		for _, p := range packages {
			owner := p.Owner.Login
			if owner == "" {
				owner = fallbackOwner
			}
			if owner == "" || p.Name == "" {
				continue
			}
			seen[strings.ToLower(owner+"/"+p.Name)] = struct{}{}
		}
	}

	var own []ghcrPackage
	if err := ghcrGetPages(ctx, client, token,
		"https://api.github.com/user/packages?package_type=container&per_page=100", &own); err != nil {
		return nil, err
	}
	add(own, username)

	var orgs []struct {
		Login string `json:"login"`
	}
	if err := ghcrGetPages(ctx, client, token, "https://api.github.com/user/orgs?per_page=100", &orgs); err != nil {
		return nil, err
	}
	for _, org := range orgs {
		var packages []ghcrPackage
		endpoint := "https://api.github.com/orgs/" + url.PathEscape(org.Login) + "/packages?package_type=container&per_page=100"
		err := ghcrGetPages(ctx, client, token, endpoint, &packages)
		// Membership alone does not grant package visibility. GitHub
		// answers 403/404 for an organization whose packages this token
		// cannot read; other failures still need to reach the operator.
		if err != nil {
			var status *ghcrStatusError
			if errors.As(err, &status) && (status.status == http.StatusForbidden || status.status == http.StatusNotFound) {
				continue
			}
			return nil, err
		}
		add(packages, org.Login)
	}

	out := make([]Repo, 0, len(seen))
	for name := range seen {
		out = append(out, Repo{Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type ghcrPackage struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type ghcrStatusError struct {
	status int
	body   string
}

func (e *ghcrStatusError) Error() string {
	return fmt.Sprintf("GitHub package API answered %d: %s", e.status, e.body)
}

// ghcrGetPages follows GitHub's Link pagination so large accounts and
// organizations do not silently lose packages after their first page.
func ghcrGetPages(ctx context.Context, client *http.Client, token, endpoint string, into any) error {
	var next = endpoint
	for next != "" {
		u, err := url.Parse(next)
		if err != nil || u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil {
			return fmt.Errorf("GitHub returned an unsafe pagination link")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "Cubeship")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("reach GitHub package API: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return errUnauthorized
		}
		if resp.StatusCode/100 != 2 {
			return &ghcrStatusError{status: resp.StatusCode, body: strings.TrimSpace(string(body))}
		}
		// Append raw page entries through the concrete destination type.
		if err := appendJSONPage(into, body); err != nil {
			return err
		}
		next = githubNext(resp.Header.Get("Link"))
	}
	return nil
}

func appendJSONPage(into any, body []byte) error {
	// Decode into the existing slice first so each API page accumulates.
	old, err := json.Marshal(into)
	if err != nil {
		return err
	}
	var combined []json.RawMessage
	if len(old) != 0 && string(old) != "null" {
		if err := json.Unmarshal(old, &combined); err != nil {
			return err
		}
	}
	var page []json.RawMessage
	if err := json.Unmarshal(body, &page); err != nil {
		return err
	}
	combined = append(combined, page...)
	encoded, err := json.Marshal(combined)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, into)
}

func githubNext(link string) string {
	for _, part := range strings.Split(link, ",") {
		sections := strings.Split(part, ";")
		if len(sections) < 2 || strings.TrimSpace(sections[1]) != `rel="next"` {
			continue
		}
		return strings.Trim(strings.TrimSpace(sections[0]), "<>")
	}
	return ""
}
