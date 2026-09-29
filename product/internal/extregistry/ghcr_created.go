package extregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// GHCR's public registry API omits push dates. OCI configs still carry
// the build time, which keeps public image tags in useful date order.
func ghcrCreatedAt(ctx context.Context, client *http.Client, c *Credential, repository string, images []Image) {
	v := newV2Client(client, c)
	scope := "repository:" + repository + ":pull"
	groups := make(map[string][]int)
	for i, image := range images {
		if image.PushedAt != nil || image.Tag == "" || strings.HasPrefix(image.Tag, "sha256-") {
			continue
		}
		key := image.Digest
		if key == "" {
			key = image.Tag
		}
		groups[key] = append(groups[key], i)
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, indices := range groups {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			created, err := v.configCreatedAt(ctx, repository, images[indices[0]].Tag, scope)
			if err != nil || created == nil {
				return
			}
			for _, i := range indices {
				images[i].CreatedAt = created
			}
		}()
	}
	wg.Wait()
}

func (v *v2Client) configCreatedAt(ctx context.Context, repository, tag, scope string) (*time.Time, error) {
	manifest := func(ref string, into any) error {
		return v.readJSON(ctx, "/v2/"+repository+"/manifests/"+ref, scope, manifestAccept, into)
	}
	var m struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := manifest(tag, &m); err != nil {
		return nil, err
	}
	if m.Config.Digest == "" {
		for _, child := range m.Manifests {
			if child.Platform.OS == "linux" && child.Platform.Architecture == "amd64" {
				if err := manifest(child.Digest, &m); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	if m.Config.Digest == "" {
		return nil, nil
	}
	var config struct {
		Created *time.Time `json:"created"`
	}
	if err := v.readJSON(ctx, "/v2/"+repository+"/blobs/"+m.Config.Digest, scope, "application/json", &config); err != nil {
		return nil, err
	}
	return config.Created, nil
}

func (v *v2Client) readJSON(ctx context.Context, path, scope, accept string, into any) error {
	resp, err := v.do(ctx, http.MethodGet, path, scope, accept)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the registry answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}
