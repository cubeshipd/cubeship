package extregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GHCR's registry API does not expose publication dates. GitHub's Packages
// API does, when the connected registry credential has read:packages access.
func ghcrPublishedAt(ctx context.Context, client *http.Client, token, repository string, images []Image) {
	if token == "" || !strings.Contains(repository, "/") {
		return
	}
	owner, pkg, _ := strings.Cut(repository, "/")
	for _, kind := range []string{"users", "orgs"} {
		for page := 1; page <= 10; page++ {
			path := fmt.Sprintf("https://api.github.com/%s/%s/packages/container/%s/versions?per_page=100&page=%d", kind, url.PathEscape(owner), url.PathEscape(pkg), page)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
			if err != nil {
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				break
			}
			var versions []struct {
				CreatedAt time.Time `json:"created_at"`
				Metadata  struct {
					Container struct {
						Tags []string `json:"tags"`
					} `json:"container"`
				} `json:"metadata"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&versions)
			resp.Body.Close()
			if err != nil {
				return
			}
			for _, version := range versions {
				for _, tag := range version.Metadata.Container.Tags {
					for i := range images {
						if images[i].Tag == tag {
							pushed := version.CreatedAt
							images[i].PushedAt = &pushed
						}
					}
				}
			}
			if len(versions) < 100 {
				return
			}
		}
	}
}
