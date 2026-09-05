package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"cubeship/internal/platform/regauth"
	"cubeship/internal/user"
)

// Repo is one repository in Cubeship's own registry, and Image one tag
// in it. The shapes match what internal/extregistry answers for a
// registry someone else runs, because what a dashboard shows is the same
// either way.
type Repo struct {
	Name string `json:"name"`
}

type Image struct {
	Tag string `json:"tag"`
}

var (
	// ErrNoRegistry reports a registry that is running but has no address
	// the daemon was told to reach it at.
	ErrNoRegistry = errors.New("the registry is not reachable from here")

	// ErrNotFound covers a repository or a tag this registry does not
	// hold, and a repository named as something it could never be.
	ErrNotFound = errors.New("not found in this registry")
)

// Repositories lists what has been pushed to this instance's registry.
//
// A repository path here *is* an app's reference —
// project/environment/app — so the catalogue is the list of apps that
// have ever had an image pushed to them.
func (h *Handler) Repositories(ctx context.Context, caller *user.User) ([]Repo, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}

	var answer struct {
		Repositories []string `json:"repositories"`
	}
	if err := h.callRegistry(ctx, "/v2/_catalog?n=1000",
		[]regauth.AccessEntry{{Type: "registry", Name: "catalog", Actions: []string{"*"}}},
		&answer); err != nil {
		return nil, err
	}

	out := []Repo{}
	for _, name := range answer.Repositories {
		out = append(out, Repo{Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Images lists one repository's tags.
func (h *Handler) Images(ctx context.Context, caller *user.User, repository string) ([]Image, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	if repository == "" {
		return nil, ErrNotFound
	}

	var answer struct {
		Tags []string `json:"tags"`
	}
	if err := h.callRegistry(ctx, "/v2/"+repository+"/tags/list",
		[]regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}},
		&answer); err != nil {
		return nil, err
	}

	out := []Image{}
	for _, tag := range answer.Tags {
		out = append(out, Image{Tag: tag})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}

// callRegistry reads from the registry container with a token the daemon
// mints for itself. No HTTP round trip through /v2/token: the key is
// already in this process.
func (h *Handler) callRegistry(ctx context.Context, path string, access []regauth.AccessEntry, into any) error {
	if h.signingKey == nil || h.localRegistry == "" {
		return ErrNoRegistry
	}
	token, err := regauth.IssueToken(h.signingKey, h.signingCert, regauth.TokenIssuer, regauth.TokenService,
		"cubeshipd", access)
	if err != nil {
		return fmt.Errorf("mint a registry token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+h.localRegistry+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoRegistry, err)
	}
	defer resp.Body.Close()

	// A repository nobody has pushed to answers 404, and that is an
	// empty list rather than a failure: an app exists before its first
	// push.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// DeleteImage removes one tag from this organization's own registry.
//
// The v2 API deletes by digest, never by tag, so the tag is resolved
// first — which means every other tag pointing at the same image goes
// with it. That is the registry's behaviour rather than a choice here,
// and it is worth knowing before asking.
//
// What this frees is nothing, immediately. Deleting a manifest only
// makes the image unreachable; the layers stay on disk until a garbage
// collection walks the storage. See GarbageCollect.
func (h *Handler) DeleteImage(ctx context.Context, caller *user.User, repository, tag string) error {
	if err := h.ownRepository(ctx, caller, repository); err != nil {
		return err
	}
	if tag == "" {
		return fmt.Errorf("name the tag to delete")
	}

	access := []regauth.AccessEntry{
		{Type: "repository", Name: repository, Actions: []string{"pull", "delete"}},
	}
	digest, err := h.digestOf(ctx, repository, tag, access)
	if err != nil {
		return err
	}
	return h.deleteManifest(ctx, repository, digest, access)
}

// DeleteRepository removes every tag in one of this organization's
// repositories.
//
// There is no delete-a-repository in the v2 API: a repository is
// whatever its manifests say it is, and it stops existing once they are
// gone. The empty name lingers in the catalogue until a garbage
// collection clears it.
func (h *Handler) DeleteRepository(ctx context.Context, caller *user.User, repository string) error {
	if err := h.ownRepository(ctx, caller, repository); err != nil {
		return err
	}

	images, err := h.Images(ctx, caller, repository)
	if err != nil {
		return err
	}

	access := []regauth.AccessEntry{
		{Type: "repository", Name: repository, Actions: []string{"pull", "delete"}},
	}
	seen := map[string]bool{}
	for _, img := range images {
		digest, err := h.digestOf(ctx, repository, img.Tag, access)
		if err != nil {
			return err
		}
		if seen[digest] {
			continue
		}
		seen[digest] = true
		if err := h.deleteManifest(ctx, repository, digest, access); err != nil {
			return err
		}
	}
	return nil
}

// ownRepository is the tenancy check every write shares. A repository
// path in this registry is an app's reference, so an organization's own
// Deleting is an admin's job. Reading the catalogue is a member's:
// removing an image is the same kind of act as deleting the app that
// pushed it.
func (h *Handler) ownRepository(ctx context.Context, caller *user.User, repository string) error {
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return err
	}
	if repository == "" {
		return ErrNotFound
	}
	return nil
}

// digestOf asks the registry what a tag currently names.
//
// It is the registry's own Docker-Content-Digest header rather than
// anything computed here: a delete addresses that exact string, and a
// byte of disagreement about how the manifest was serialized would make
// a computed one wrong.
func (h *Handler) digestOf(ctx context.Context, repository, tag string, access []regauth.AccessEntry) (string, error) {
	resp, err := h.registryRequest(ctx, http.MethodHead, "/v2/"+repository+"/manifests/"+tag, access)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("the registry answered %s", resp.Status)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("the registry did not say what %s:%s is, so there is nothing to address", repository, tag)
	}
	return digest, nil
}

func (h *Handler) deleteManifest(ctx context.Context, repository, digest string, access []regauth.AccessEntry) error {
	resp, err := h.registryRequest(ctx, http.MethodDelete, "/v2/"+repository+"/manifests/"+digest, access)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	// The registry is configured with storage.delete.enabled, so a 405
	// means it is running from an older configuration than this daemon
	// writes — which restarting it fixes.
	case resp.StatusCode == http.StatusMethodNotAllowed:
		return fmt.Errorf("this registry is running without deletion enabled; restarting the daemon reconfigures it")
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return nil
}

// registryRequest is callRegistry for the calls that read a header or a
// status rather than a JSON body.
func (h *Handler) registryRequest(ctx context.Context, method, path string, access []regauth.AccessEntry) (*http.Response, error) {
	if h.signingKey == nil || h.localRegistry == "" {
		return nil, ErrNoRegistry
	}
	token, err := regauth.IssueToken(h.signingKey, h.signingCert, regauth.TokenIssuer, regauth.TokenService,
		"cubeshipd", access)
	if err != nil {
		return nil, fmt.Errorf("mint a registry token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://"+h.localRegistry+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", manifestAccept)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoRegistry, err)
	}
	return resp, nil
}

// manifestAccept names every manifest type worth asking for. A request
// that does not name the OCI types gets a v1 manifest — with a different
// digest, which would delete the wrong thing — or a 404 for an image
// that only exists as an index.
const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json"
