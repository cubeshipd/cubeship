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
	"sync"
)

// The Docker Registry v2 API, which is what everything that is not ECR
// speaks — DigitalOcean, Harbor, GitLab, a plain registry:2.
//
// ECR is the exception rather than the rule: it has an AWS API of its
// own, and its v2 endpoint answers only with a token minted through
// that API. So AWS goes through aws.go and everything else through
// here, and both arrive at the same Repo and Image.
//
// Authentication is a handshake rather than a header. An anonymous
// request is refused with a WWW-Authenticate naming a token server; the
// stored login buys a token there, scoped to the one thing being asked
// for, and the token is what the registry accepts. Sending the password
// straight to the registry would work on some and be silently ignored
// by the rest.

// manifestAccept lists every manifest media type worth asking for. A
// registry serves the first one it has, and a request that does not name
// the OCI types gets a v1 manifest — or a 404 for an image that only
// exists as an index.
const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json"

// v2Client talks to one registry with one login.
type v2Client struct {
	http     *http.Client
	host     string
	username string
	password string

	mu     sync.Mutex
	tokens map[string]string // scope -> bearer token
}

func newV2Client(client *http.Client, c *Credential) *v2Client {
	username, password := c.Login()
	return &v2Client{
		http:     client,
		host:     c.Host,
		username: username,
		password: password,
		tokens:   map[string]string{},
	}
}

// errUnauthorized is a login the registry refused, as opposed to a
// registry that could not be reached at all. The difference is the whole
// point of the status column: one is fixed by re-authenticating and the
// other is not.
var errUnauthorized = errors.New("the registry refused this login")

// do performs one request for a scope, buying a token if it has to.
//
// The token is bought lazily and only once per scope: a registry that
// needs none — an open mirror, a Harbor with anonymous pull — answers
// the first attempt, and asking a token server it never named would fail
// for no reason.
func (v *v2Client) do(ctx context.Context, method, path, scope string, accept string) (*http.Response, error) {
	send := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, "https://"+v.host+path, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return v.http.Do(req)
	}

	v.mu.Lock()
	token := v.tokens[scope]
	v.mu.Unlock()

	resp, err := send(token)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", v.host, err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// Refused. The refusal itself says where to go for a token.
	challenge := resp.Header.Get("Www-Authenticate")
	resp.Body.Close()
	realm, service := parseChallenge(challenge)
	if realm == "" {
		return nil, errUnauthorized
	}

	token, err = v.mintToken(ctx, realm, service, scope)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	v.tokens[scope] = token
	v.mu.Unlock()

	resp, err = send(token)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", v.host, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errUnauthorized
	}
	return resp, nil
}

// mintToken exchanges the stored login for a scoped bearer token.
func (v *v2Client) mintToken(ctx context.Context, realm, service, scope string) (string, error) {
	u, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("the registry named an unusable token server: %w", err)
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if v.username != "" || v.password != "" {
		req.SetBasicAuth(v.username, v.password)
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach the token server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the token server answered %s", resp.Status)
	}

	// Two names for one field: the spec says "token", and Docker's own
	// implementation also writes "access_token".
	var answer struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return "", fmt.Errorf("read the token: %w", err)
	}
	if answer.Token != "" {
		return answer.Token, nil
	}
	if answer.AccessToken == "" {
		return "", errUnauthorized
	}
	return answer.AccessToken, nil
}

// parseChallenge reads realm and service out of a WWW-Authenticate.
func parseChallenge(header string) (realm, service string) {
	rest, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return "", ""
	}
	for _, part := range strings.Split(rest, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "realm":
			realm = value
		case "service":
			service = value
		}
	}
	return realm, service
}

// getJSON runs a GET for a scope and decodes what comes back.
func (v *v2Client) getJSON(ctx context.Context, path, scope string, into any) error {
	resp, err := v.do(ctx, http.MethodGet, path, scope, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

// pingV2 is the status probe: it asks the registry for the one thing
// every v2 registry has, and reports what came back.
func pingV2(ctx context.Context, client *http.Client, c *Credential) error {
	v := newV2Client(client, c)
	resp, err := v.do(ctx, http.MethodGet, "/v2/", "", "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return nil
}

// listV2Repositories reads the catalogue.
//
// A namespace narrows it: DigitalOcean puts every repository under the
// registry's own name, and someone looking at registry.digitalocean.com/vxcase
// means the ones under vxcase.
func listV2Repositories(ctx context.Context, client *http.Client, c *Credential) ([]Repo, error) {
	v := newV2Client(client, c)

	var answer struct {
		Repositories []string `json:"repositories"`
	}
	if err := v.getJSON(ctx, "/v2/_catalog?n=1000", "registry:catalog:*", &answer); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoListing
		}
		return nil, err
	}

	prefix := ""
	if c.Namespace != "" {
		prefix = c.Namespace + "/"
	}

	out := make([]Repo, 0, len(answer.Repositories))
	for _, name := range answer.Repositories {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		out = append(out, Repo{Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// listV2Images reads one repository's tags, with the size of each.
//
// The tag list is one call; a size is one call per tag, because the v2
// API keeps sizes in the manifest rather than in any listing. They run
// concurrently and a tag whose manifest cannot be read is still listed —
// a missing size is worth less than a missing tag.
func listV2Images(ctx context.Context, client *http.Client, c *Credential, repository string) ([]Image, error) {
	v := newV2Client(client, c)
	scope := "repository:" + repository + ":pull"

	var answer struct {
		Tags []string `json:"tags"`
	}
	if err := v.getJSON(ctx, "/v2/"+repository+"/tags/list?n=1000", scope, &answer); err != nil {
		return nil, err
	}

	out := make([]Image, len(answer.Tags))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, tag := range answer.Tags {
		out[i] = Image{Tag: tag}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			digest, size, err := v.manifest(ctx, repository, tag, scope)
			if err != nil {
				return
			}
			out[i].Digest, out[i].Size = digest, size
		}()
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}

// manifest returns a tag's digest and the size of everything it names.
//
// The digest is the registry's own header rather than something computed
// here: it is what a delete addresses, and a byte of disagreement about
// how the manifest was serialized would make a computed one wrong.
func (v *v2Client) manifest(ctx context.Context, repository, reference, scope string) (digest string, size int64, err error) {
	resp, err := v.do(ctx, http.MethodGet, "/v2/"+repository+"/manifests/"+reference, scope, manifestAccept)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", 0, ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("the registry answered %s", resp.Status)
	}

	var m struct {
		Config struct {
			Size int64 `json:"size"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&m); err != nil {
		return "", 0, fmt.Errorf("read the manifest: %w", err)
	}

	size = m.Config.Size
	for _, l := range m.Layers {
		size += l.Size
	}
	return resp.Header.Get("Docker-Content-Digest"), size, nil
}

// deleteV2Image removes one tag.
//
// The v2 API deletes by digest, never by tag, so the tag is resolved
// first. That means deleting a tag deletes the image, and every other
// tag pointing at the same image goes with it — which is what the
// registry does and worth knowing before you ask for it.
func deleteV2Image(ctx context.Context, client *http.Client, c *Credential, repository string, ref ImageRef) error {
	v := newV2Client(client, c)
	scope := "repository:" + repository + ":pull,push,delete"

	digest := ref.Digest
	if ref.Tag != "" {
		// Resolved rather than trusted: a tag can have moved since the
		// listing was read, and the digest that comes back with the
		// manifest is the one this registry will accept.
		var err error
		if digest, _, err = v.manifest(ctx, repository, ref.Tag, scope); err != nil {
			return err
		}
	}
	if digest == "" {
		return fmt.Errorf("the registry did not say what %s is, so there is nothing to address", ref.In(repository))
	}
	return v.deleteManifest(ctx, repository, digest, scope)
}

// deleteV2Repository removes every tag in a repository.
//
// The v2 API has no delete-a-repository: a repository is whatever its
// manifests say it is, and it stops existing once they are gone. What is
// left behind is the empty name in the catalogue, which garbage
// collection clears.
func deleteV2Repository(ctx context.Context, client *http.Client, c *Credential, repository string) error {
	images, err := listV2Images(ctx, client, c, repository)
	if err != nil {
		return err
	}

	v := newV2Client(client, c)
	scope := "repository:" + repository + ":pull,push,delete"

	seen := map[string]bool{}
	for _, img := range images {
		digest := img.Digest
		if digest == "" {
			if digest, _, err = v.manifest(ctx, repository, img.Tag, scope); err != nil {
				return err
			}
		}
		if seen[digest] {
			continue
		}
		seen[digest] = true
		if err := v.deleteManifest(ctx, repository, digest, scope); err != nil {
			return err
		}
	}
	return nil
}

// deleteManifest is the one call that removes an image, and the one
// place a registry with deletion turned off says so.
func (v *v2Client) deleteManifest(ctx context.Context, repository, digest, scope string) error {
	resp, err := v.do(ctx, http.MethodDelete, "/v2/"+repository+"/manifests/"+digest, scope, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	// A registry built without deletion answers 405. Nothing here can
	// change that, and saying which registry refused beats "failed".
	case resp.StatusCode == http.StatusMethodNotAllowed:
		return fmt.Errorf("%s does not allow deleting images", v.host)
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return nil
}

// usageV2 adds up what a registry's images occupy.
//
// Layers shared between images are counted once per image that names
// them, so the total is an upper bound rather than what the disk holds.
func usageV2(ctx context.Context, client *http.Client, c *Credential) (*Usage, error) {
	repos, err := listV2Repositories(ctx, client, c)
	if err != nil {
		return nil, err
	}

	out := &Usage{Shared: true, Repositories: make([]RepoUsage, len(repos))}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, r := range repos {
		out.Repositories[i] = RepoUsage{Name: r.Name}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			images, err := listV2Images(ctx, client, c, r.Name)
			if err != nil {
				return
			}
			for _, img := range images {
				out.Repositories[i].Count++
				out.Repositories[i].Bytes += img.Size
			}
		}()
	}
	wg.Wait()

	for _, r := range out.Repositories {
		out.TotalBytes += r.Bytes
	}
	return out, nil
}
