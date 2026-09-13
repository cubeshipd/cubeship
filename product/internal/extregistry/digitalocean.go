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
	"time"
)

// DigitalOcean's container registry through DigitalOcean's own API
// rather than the Registry v2 one.
//
// It speaks v2 — that is what `docker pull` uses — but its catalogue is
// not reachable that way: the token its auth server issues for a
// personal access token does not carry `registry:catalog:*`, so asking
// for the catalogue answers with an empty list rather than a refusal.
// A registry full of repositories reading as empty is worse than an
// error, which is why this exists.
//
// The API is also simply better for the job. It reports a repository's
// tag count, and every manifest's size and when it was pushed — figures
// the v2 API keeps inside manifests that would be one request each.
//
// The credential is a DigitalOcean personal access token. Their
// instructions have you use it as both the username and the password to
// `docker login`, so the password field is the token, and it is the same
// bearer credential their API takes.

const doAPI = "https://api.digitalocean.com"

// doCall runs one DigitalOcean API request.
func doCall(ctx context.Context, client *http.Client, c *Credential, method, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, doAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach DigitalOcean: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		// DigitalOcean says why in the body, and that sentence is the
		// whole value of the refusal: a token that is not a token and a
		// token missing a scope both arrive as 401, and only this tells
		// them apart. Dropping it leaves "refused" and nothing to act on.
		var problem struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&problem)
		refusal := problem.Message
		if refusal == "" {
			refusal = resp.Status
		}

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: DigitalOcean said %q for %s", errUnauthorized, refusal, path)
		case http.StatusNotFound:
			return ErrNotFound
		}
		return fmt.Errorf("DigitalOcean refused %s: %s", path, refusal)
	}

	if into == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

// doRegistry is the registry's name, which is the path segment between
// the host and the image — registry.digitalocean.com/<name>/<image>.
func doRegistry(c *Credential) (string, error) {
	if c.Namespace == "" {
		return "", fmt.Errorf("this login does not say which DigitalOcean registry it is for")
	}
	return url.PathEscape(c.Namespace), nil
}

// listDORepositories asks DigitalOcean what the registry holds.
func listDORepositories(ctx context.Context, client *http.Client, c *Credential) ([]Repo, error) {
	name, err := doRegistry(c)
	if err != nil {
		return nil, err
	}

	var answer struct {
		Repositories []struct {
			Name string `json:"name"`
		} `json:"repositories"`
	}
	path := "/v2/registry/" + name + "/repositoriesV2?per_page=200"
	if err := doCall(ctx, client, c, http.MethodGet, path, &answer); err != nil {
		return nil, err
	}

	// The names come back bare — `myapp`, not `vxcase/myapp` — and
	// everything downstream addresses a repository the way an image
	// reference spells it, so the registry's name goes back on.
	out := make([]Repo, 0, len(answer.Repositories))
	for _, r := range answer.Repositories {
		out = append(out, Repo{Name: c.Namespace + "/" + r.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// doManifest is one image, as DigitalOcean reports it.
type doManifest struct {
	Digest string `json:"digest"`
	// CompressedSizeBytes is what the registry stores, which is what the
	// account is billed for. SizeBytes is the image unpacked, and is the
	// larger and less useful of the two here.
	CompressedSizeBytes int64     `json:"compressed_size_bytes"`
	UpdatedAt           time.Time `json:"updated_at"`
	Tags                []string  `json:"tags"`
}

// listDOManifests reads every manifest in a repository.
func listDOManifests(ctx context.Context, client *http.Client, c *Credential, repository string) ([]doManifest, error) {
	name, err := doRegistry(c)
	if err != nil {
		return nil, err
	}

	var answer struct {
		Manifests []doManifest `json:"manifests"`
	}
	path := "/v2/registry/" + name + "/repositories/" + url.PathEscape(bareRepository(c, repository)) +
		"/digests?per_page=200"
	if err := doCall(ctx, client, c, http.MethodGet, path, &answer); err != nil {
		return nil, err
	}
	return answer.Manifests, nil
}

// listDOImages lists a repository's tags, with size and date.
//
// An untagged manifest is left out. It still occupies disk — which is
// what a garbage collection over there is for — but it is not something
// anything can pull, and a listing of tags is what this answers.
func listDOImages(ctx context.Context, client *http.Client, c *Credential, repository string) ([]Image, error) {
	manifests, err := listDOManifests(ctx, client, c, repository)
	if err != nil {
		return nil, err
	}

	out := []Image{}
	for _, m := range manifests {
		for _, tag := range m.Tags {
			pushed := m.UpdatedAt
			out = append(out, Image{
				Tag:      tag,
				Digest:   m.Digest,
				Size:     m.CompressedSizeBytes,
				PushedAt: &pushed,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}

// bareRepository strips the registry's name back off, because that is
// how the API addresses a repository even though an image reference
// carries it.
func bareRepository(c *Credential, repository string) string {
	if c.Namespace == "" {
		return repository
	}
	if rest, ok := cutPrefix(repository, c.Namespace+"/"); ok {
		return rest
	}
	return repository
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// deleteDOImage removes one image, by tag or by digest.
//
// DigitalOcean deletes a tag as a tag, unlike the Registry v2 API, so
// other tags on the same image survive it. An untagged image has only
// its digest, and DigitalOcean has a separate endpoint for that. What
// neither does is free anything: the manifest and its blobs stay until
// a garbage collection runs over there, which is DigitalOcean's to
// start.
func deleteDOImage(ctx context.Context, client *http.Client, c *Credential, repository string, ref ImageRef) error {
	name, err := doRegistry(c)
	if err != nil {
		return err
	}
	base := "/v2/registry/" + name + "/repositories/" + url.PathEscape(bareRepository(c, repository))
	if ref.Tag == "" {
		return doCall(ctx, client, c, http.MethodDelete, base+"/digests/"+url.PathEscape(ref.Digest), nil)
	}
	return doCall(ctx, client, c, http.MethodDelete, base+"/tags/"+url.PathEscape(ref.Tag), nil)
}

// deleteDORepository removes every manifest in a repository, which is
// what makes the repository stop existing.
func deleteDORepository(ctx context.Context, client *http.Client, c *Credential, repository string) error {
	manifests, err := listDOManifests(ctx, client, c, repository)
	if err != nil {
		return err
	}
	name, err := doRegistry(c)
	if err != nil {
		return err
	}

	base := "/v2/registry/" + name + "/repositories/" + url.PathEscape(bareRepository(c, repository)) + "/digests/"
	for _, m := range manifests {
		if err := doCall(ctx, client, c, http.MethodDelete, base+url.PathEscape(m.Digest), nil); err != nil {
			// A manifest already gone is the outcome asked for.
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

// doUsage adds up what the registry stores.
//
// Unlike the v2 path, this counts manifests rather than tags — two tags
// on one image are one image on disk — so it does not double-count the
// way a tag-by-tag sum would.
func doUsage(ctx context.Context, client *http.Client, c *Credential) (*Usage, error) {
	repos, err := listDORepositories(ctx, client, c)
	if err != nil {
		return nil, err
	}

	out := &Usage{Shared: true, Repositories: make([]RepoUsage, 0, len(repos))}
	for _, r := range repos {
		manifests, err := listDOManifests(ctx, client, c, r.Name)
		if err != nil {
			return nil, err
		}
		usage := RepoUsage{Name: r.Name, Count: len(manifests)}
		for _, m := range manifests {
			usage.Bytes += m.CompressedSizeBytes
		}
		out.Repositories = append(out.Repositories, usage)
		out.TotalBytes += usage.Bytes
	}
	return out, nil
}

// pingDO is the status probe, and it asks for the catalogue rather than
// for the registry's record.
//
// The record only proves the token exists. Listing is what the dashboard
// does the moment you click the row, and it can fail on its own — a
// scoped token, the wrong registry name — so probing the cheaper call
// would report available for a row that opens on an error.
//
// A token that cannot list but can still pull is not a broken login,
// though, and a deploy is what a status is really about. So a refused
// listing is checked against the record: if that answers, the login
// works and the listing is a separate problem, reported where someone
// can read it.
func pingDO(ctx context.Context, client *http.Client, c *Credential) error {
	_, err := listDORepositories(ctx, client, c)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errUnauthorized) {
		return err
	}
	if record := doCall(ctx, client, c, http.MethodGet, "/v2/registry", nil); record == nil {
		return nil
	}
	return err
}
