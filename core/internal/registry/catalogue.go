package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
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
	Tag       string     `json:"tag"`
	Digest    string     `json:"digest,omitempty"`
	Size      int64      `json:"size,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
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
	if err := user.Allow(caller, user.ResRegistry, user.LevelView, ""); err != nil {
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
	if err := user.Allow(caller, user.ResRegistry, user.LevelView, ""); err != nil {
		return nil, err
	}
	if repository == "" {
		return nil, ErrNotFound
	}

	tags, err := h.tags(ctx, repository)
	if err != nil {
		return nil, err
	}

	out := make([]Image, len(tags))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, tag := range tags {
		out[i] = Image{Tag: tag}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// A tag remains visible if its metadata cannot be read.
			if metadata, err := h.imageMetadata(ctx, repository, tag); err == nil {
				out[i] = metadata
			}
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}

func (h *Handler) tags(ctx context.Context, repository string) ([]string, error) {
	var answer struct {
		Tags []string `json:"tags"`
	}
	err := h.callRegistry(ctx, "/v2/"+repository+"/tags/list",
		[]regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}}, &answer)
	return answer.Tags, err
}

type imageDescriptor struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type imageManifest struct {
	Config    imageDescriptor   `json:"config"`
	Layers    []imageDescriptor `json:"layers"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS string `json:"os"`
		} `json:"platform"`
	} `json:"manifests"`
}

func (h *Handler) imageMetadata(ctx context.Context, repository, tag string) (Image, error) {
	access := []regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}}
	manifest, digest, err := h.readManifest(ctx, repository, tag, access)
	if err != nil {
		return Image{}, err
	}
	image := Image{Tag: tag, Digest: digest}
	seen := map[string]bool{}
	var configDigest string
	add := func(m imageManifest) {
		if configDigest == "" {
			configDigest = m.Config.Digest
		}
		for _, blob := range append([]imageDescriptor{m.Config}, m.Layers...) {
			if blob.Digest != "" && !seen[blob.Digest] && blob.Size > 0 {
				image.Size += blob.Size
				seen[blob.Digest] = true
			}
		}
	}
	if manifest.Config.Digest != "" {
		add(manifest)
	} else {
		for _, child := range manifest.Manifests {
			if child.Platform.OS == "unknown" {
				continue // BuildKit attestations are not runnable images.
			}
			m, _, err := h.readManifest(ctx, repository, child.Digest, access)
			if err != nil {
				return Image{}, err
			}
			add(m)
		}
	}
	if configDigest != "" {
		var config struct {
			Created *time.Time `json:"created"`
		}
		if err := h.readRegistryJSON(ctx, "/v2/"+repository+"/blobs/"+configDigest, access, &config); err == nil {
			image.CreatedAt = config.Created
		}
	}
	return image, nil
}

func (h *Handler) readManifest(ctx context.Context, repository, reference string, access []regauth.AccessEntry) (imageManifest, string, error) {
	var manifest imageManifest
	resp, err := h.registryRequest(ctx, http.MethodGet, "/v2/"+repository+"/manifests/"+reference, access)
	if err != nil {
		return manifest, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return manifest, "", fmt.Errorf("the registry answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&manifest); err != nil {
		return manifest, "", err
	}
	return manifest, resp.Header.Get("Docker-Content-Digest"), nil
}

func (h *Handler) readRegistryJSON(ctx context.Context, path string, access []regauth.AccessEntry, into any) error {
	resp, err := h.registryRequest(ctx, http.MethodGet, path, access)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
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

// DeleteImage removes one tag from this instance's registry.
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

// DeleteRepository removes every tag in a repository on this instance.
//
// There is no delete-a-repository in the v2 API: a repository is
// whatever its manifests say it is, and it stops existing once they are
// gone. The empty name lingers in the catalogue until a garbage
// collection clears it.
func (h *Handler) DeleteRepository(ctx context.Context, caller *user.User, repository string) error {
	if err := h.ownRepository(ctx, caller, repository); err != nil {
		return err
	}

	tags, err := h.tags(ctx, repository)
	if err != nil {
		return err
	}

	access := []regauth.AccessEntry{
		{Type: "repository", Name: repository, Actions: []string{"pull", "delete"}},
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		digest, err := h.digestOf(ctx, repository, tag, access)
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

// ownRepository checks the caller's role and the repository name.
// Deleting is an admin's job. Reading the catalogue is a member's:
// removing an image is the same kind of act as deleting the app that
// pushed it.
func (h *Handler) ownRepository(ctx context.Context, caller *user.User, repository string) error {
	if err := user.Allow(caller, user.ResRegistry, user.LevelManage, ""); err != nil {
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
