package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cubeship/internal/release"
)

// ReleasesAPI is where newer releases are looked up.
//
// **This is the one thing here that reaches the internet, and it has
// to.** A build knows every release up to its own — they are in it —
// and by definition knows nothing about the one that came after. The
// changelog is carried so a dialog works on a box behind a firewall;
// this cannot be, because the answer did not exist when the binary was
// made.
//
// So an instance that cannot reach GitHub simply never offers an
// update, and says so rather than pretending it is current.
const ReleasesAPI = "https://api.github.com/repos/cubeshipd/cubeship/releases"

// checkTimeout bounds the lookup. It is short: nobody is waiting on it,
// and a slow answer is the same as no answer to a screen that has
// already rendered.
const checkTimeout = 10 * time.Second

// Available is a release this instance could move to.
type Available struct {
	Version string `json:"version"`
	// Notes is the release's own body, as written. It is the one thing
	// about a newer version this instance cannot have in its binary,
	// which is why it is carried here.
	Notes string `json:"notes,omitempty"`
	// PublishedAt is when it went out.
	PublishedAt time.Time `json:"published_at,omitempty"`
	// Prerelease says it is a candidate. Never offered by a check —
	// see Newer — and reachable by asking for it by name.
	Prerelease bool `json:"prerelease,omitempty"`
}

// Newer is the newest stable release above current, or nil.
//
// **Prereleases are skipped.** Somebody running a candidate asked for
// it by name, and nobody on a stable release should be offered one by a
// button that says "update".
func Newer(ctx context.Context, client *http.Client, current string) (*Available, error) {
	all, err := list(ctx, client)
	if err != nil {
		return nil, err
	}
	current = release.Normalize(current)
	if current == "" {
		// A build with nothing stamped on it cannot be compared to
		// anything, and offering it "the newest release" would be
		// offering to replace somebody's own build with a published
		// one.
		return nil, nil
	}
	var best *Available
	for i := range all {
		r := all[i]
		if r.Prerelease || release.Compare(r.Version, current) <= 0 {
			continue
		}
		if best == nil || release.Compare(r.Version, best.Version) > 0 {
			best = &r
		}
	}
	return best, nil
}

// Find is one release by version, prerelease or not.
//
// Asking for a candidate by name is the supported way to run one, which
// is why this does not filter and Newer does.
func Find(ctx context.Context, client *http.Client, version string) (*Available, error) {
	all, err := list(ctx, client)
	if err != nil {
		return nil, err
	}
	want := release.Normalize(version)
	for i := range all {
		if all[i].Version == want {
			return &all[i], nil
		}
	}
	return nil, ErrUnknownVersion
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

func list(ctx context.Context, client *http.Client) ([]Available, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesAPI+"?per_page=20", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask which releases exist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ask which releases exist: %s answered %s", ReleasesAPI, resp.Status)
	}

	var raw []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("read which releases exist: %w", err)
	}
	out := make([]Available, 0, len(raw))
	for _, r := range raw {
		v := release.Normalize(r.TagName)
		if r.Draft || v == "" {
			continue
		}
		out = append(out, Available{
			Version: v, Notes: r.Body,
			PublishedAt: r.PublishedAt, Prerelease: r.Prerelease,
		})
	}
	return out, nil
}
