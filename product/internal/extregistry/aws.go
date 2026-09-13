package extregistry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cubeship/internal/platform/awssig"

	"golang.org/x/sync/errgroup"
)

// ECR is the one registry whose credential is not a password.
//
// What an operator holds is an access key; what Docker logs in with is a
// token AWS mints from it, good for twelve hours. So the key is stored
// and the token is fetched — which is also what makes the registry's own
// address discoverable, because the same call answers with it.
//
// The signing below is SigV4 for exactly one request. A hand-rolled
// hundred lines against the AWS SDK, which is one of the largest
// dependencies in Go, for a single unchanging call.

// awsAuth is what an ECR login comes back as.
type awsAuth struct {
	Username string
	Password string
	// Registry is the host AWS says this account's ECR is at. It
	// carries the account id, which is why it is discovered rather than
	// asked for.
	Registry string
	Expires  time.Time
}

// ecrEndpoint is where GetAuthorizationToken is called. A variable so a
// test can point it somewhere it controls.
var ecrEndpoint = func(region string) string {
	return fmt.Sprintf("https://api.ecr.%s.amazonaws.com/", region)
}

// getECRAuthorization exchanges an access key for a registry login.
func getECRAuthorization(ctx context.Context, client *http.Client, accessKeyID, secret, region string) (*awsAuth, error) {
	const (
		service = "ecr"
		target  = "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken"
	)
	body := []byte(`{}`)
	endpoint := ecrEndpoint(region)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", target)

	awssig.Sign(req, body, accessKeyID, secret, region, service, time.Now())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask AWS for an ECR token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// AWS names the reason in the body, and the status alone does
		// not distinguish a wrong key from a key with no ECR
		// permission.
		var problem struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&problem)
		if problem.Message != "" {
			return nil, fmt.Errorf("AWS refused: %s", problem.Message)
		}
		return nil, fmt.Errorf("AWS refused the ECR token request: %s", resp.Status)
	}

	// expiresAt is a number, not a timestamp string: AWS's JSON 1.1
	// protocol serializes times as epoch seconds, fractional part and
	// all.
	var answer struct {
		AuthorizationData []struct {
			AuthorizationToken string  `json:"authorizationToken"`
			ProxyEndpoint      string  `json:"proxyEndpoint"`
			ExpiresAt          float64 `json:"expiresAt"`
		} `json:"authorizationData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return nil, fmt.Errorf("decode AWS's answer: %w", err)
	}
	if len(answer.AuthorizationData) == 0 {
		return nil, fmt.Errorf("AWS returned no ECR authorization; the key may have no registry in %s", region)
	}

	data := answer.AuthorizationData[0]
	// The token is base64 of "user:password", and the user is always
	// AWS. Decoding it is what Docker would do.
	decoded, err := base64.StdEncoding.DecodeString(data.AuthorizationToken)
	if err != nil {
		return nil, fmt.Errorf("decode the ECR token: %w", err)
	}
	user, password, found := strings.Cut(string(decoded), ":")
	if !found {
		return nil, fmt.Errorf("the ECR token is not user:password")
	}

	expires := time.Unix(int64(data.ExpiresAt), 0)
	if data.ExpiresAt == 0 {
		// Nothing said when it dies. Twelve hours is what ECR gives, and
		// treating it as already expired would fetch a new one per pull.
		expires = time.Now().Add(12 * time.Hour)
	}

	return &awsAuth{
		Username: user,
		Password: password,
		Registry: strings.TrimPrefix(data.ProxyEndpoint, "https://"),
		Expires:  expires,
	}, nil
}

// Repo is one repository in a registry, and Image one tag in it. They
// are the same shape whichever registry answered, because what the
// dashboard shows is the same either way.
type Repo struct {
	Name string `json:"name"`
}

type Image struct {
	Tag string `json:"tag"`
	// Digest identifies the image itself. A tag can move; this does not.
	Digest string `json:"digest,omitempty"`
	// Size is the image's size in bytes, where the registry reports one.
	Size int64 `json:"size,omitempty"`
	// PushedAt is when it arrived, where the registry reports it.
	PushedAt *time.Time `json:"pushed_at,omitempty"`
}

// listECRRepositories asks ECR what is in the registry.
func listECRRepositories(ctx context.Context, client *http.Client, c *Credential) ([]Repo, error) {
	var answer struct {
		Repositories []struct {
			RepositoryName string `json:"repositoryName"`
		} `json:"repositories"`
	}
	if err := callECR(ctx, client, c, "DescribeRepositories", `{"maxResults":1000}`, &answer); err != nil {
		return nil, err
	}

	out := make([]Repo, 0, len(answer.Repositories))
	for _, r := range answer.Repositories {
		out = append(out, Repo{Name: r.RepositoryName})
	}
	return out, nil
}

// listECRImages asks ECR for one repository's tags.
func listECRImages(ctx context.Context, client *http.Client, c *Credential, repository string) ([]Image, error) {
	body, err := json.Marshal(map[string]any{
		"repositoryName": repository,
		"maxResults":     1000,
	})
	if err != nil {
		return nil, err
	}

	var answer struct {
		ImageDetails []struct {
			ImageDigest      string   `json:"imageDigest"`
			ImageTags        []string `json:"imageTags"`
			ImageSizeInBytes int64    `json:"imageSizeInBytes"`
			ImagePushedAt    float64  `json:"imagePushedAt"`
		} `json:"imageDetails"`
	}
	if err := callECR(ctx, client, c, "DescribeImages", string(body), &answer); err != nil {
		return nil, err
	}

	out := []Image{}
	for _, d := range answer.ImageDetails {
		pushed := time.Unix(int64(d.ImagePushedAt), 0)
		if len(d.ImageTags) == 0 {
			// An untagged image is still occupying the registry, and a
			// listing that hid it would not add up against the console.
			//
			// Its Tag is empty rather than a stand-in like "<untagged>":
			// a stand-in is a name two different images would share, and
			// it is not something a delete could ever address. Nothing
			// but the digest identifies one of these.
			out = append(out, Image{Digest: d.ImageDigest,
				Size: d.ImageSizeInBytes, PushedAt: &pushed})
			continue
		}
		for _, tag := range d.ImageTags {
			out = append(out, Image{Tag: tag, Digest: d.ImageDigest,
				Size: d.ImageSizeInBytes, PushedAt: &pushed})
		}
	}
	// Newest first. A registry's list is read to find what was pushed
	// recently far more often than to find a tag alphabetically.
	sort.Slice(out, func(i, j int) bool {
		if out[i].PushedAt == nil || out[j].PushedAt == nil {
			return out[i].Tag < out[j].Tag
		}
		return out[i].PushedAt.After(*out[j].PushedAt)
	})
	return out, nil
}

// callECR is one signed request to the ECR API.
func callECR(ctx context.Context, client *http.Client, c *Credential, action, body string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ecrEndpoint(c.Region), strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AmazonEC2ContainerRegistry_V20150921."+action)

	awssig.Sign(req, []byte(body), c.Username, c.Password, c.Region, "ecr", time.Now())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ask AWS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&problem)

		refusal := problem.Message
		if refusal == "" {
			refusal = resp.Status
		}
		if awsRejectedTheKey(resp.StatusCode, problem.Type) {
			return fmt.Errorf("%w: %s", errAWSUnauthorized, refusal)
		}
		return fmt.Errorf("AWS refused: %s", refusal)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// deleteECRImage removes one image, addressed by tag or by digest.
//
// A tag is what a person picked off a list, and deleting it leaves every
// other tag on the same image alone — which is why it is preferred where
// there is one. An untagged image has no tag to name, and the digest is
// the only thing that identifies it.
func deleteECRImage(ctx context.Context, client *http.Client, c *Credential, repository string, ref ImageRef) error {
	id := map[string]string{"imageTag": ref.Tag}
	if ref.Tag == "" {
		id = map[string]string{"imageDigest": ref.Digest}
	}
	body, err := json.Marshal(map[string]any{
		"repositoryName": repository,
		"imageIds":       []map[string]string{id},
	})
	if err != nil {
		return err
	}

	var answer struct {
		Failures []struct {
			FailureCode   string `json:"failureCode"`
			FailureReason string `json:"failureReason"`
		} `json:"failures"`
	}
	if err := callECR(ctx, client, c, "BatchDeleteImage", string(body), &answer); err != nil {
		return err
	}
	// A batch of one that failed comes back 200 with the reason inside,
	// so the transport succeeding is not the delete succeeding.
	if len(answer.Failures) > 0 {
		return fmt.Errorf("AWS refused to delete %s: %s",
			ref.In(repository), answer.Failures[0].FailureReason)
	}
	return nil
}

// deleteECRRepository removes a repository and everything in it.
func deleteECRRepository(ctx context.Context, client *http.Client, c *Credential, repository string) error {
	body, err := json.Marshal(map[string]any{
		"repositoryName": repository,
		// Without this AWS refuses a repository that still holds
		// images, and the caller has already been told what goes.
		"force": true,
	})
	if err != nil {
		return err
	}
	return callECR(ctx, client, c, "DeleteRepository", string(body), new(struct{}))
}

// Usage is how much a registry's images add up to.
//
// It is a sum of per-image sizes, and layers are shared between images:
// two tags built from one base count that base twice. So this is an
// upper bound on what the registry stores, not what it bills — and the
// gap is large on a repository with many tags of the same app.
type Usage struct {
	TotalBytes   int64       `json:"total_bytes"`
	Repositories []RepoUsage `json:"repositories"`
	// Shared says the number double-counts layers shared between
	// images. It is always true here, and it is in the payload so a
	// caller cannot present the figure as exact without having seen it.
	Shared bool `json:"counts_shared_layers"`
}

type RepoUsage struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Count int    `json:"images"`
}

// ecrUsage measures every repository, a few at a time.
//
// ECR has no aggregate to ask for, so this is one call per repository.
// Bounded because a hundred and seventy signed requests at once is a
// good way to be rate-limited by someone else's API.
func ecrUsage(ctx context.Context, client *http.Client, c *Credential, repos []Repo) (*Usage, error) {
	const parallel = 8

	out := Usage{Repositories: make([]RepoUsage, len(repos)), Shared: true}
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(parallel)

	for i, repo := range repos {
		group.Go(func() error {
			images, err := listECRImages(ctx, client, c, repo.Name)
			if err != nil {
				return err
			}
			// One image can carry several tags, and listECRImages
			// returns a row per tag. Counting by digest is what stops a
			// three-tag image being measured three times.
			seen := map[string]bool{}
			usage := RepoUsage{Name: repo.Name}
			for _, image := range images {
				key := image.Digest
				if key == "" {
					key = image.Tag
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				usage.Bytes += image.Size
				usage.Count++
			}
			out.Repositories[i] = usage
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	for _, r := range out.Repositories {
		out.TotalBytes += r.Bytes
	}
	return &out, nil
}

// errAWSUnauthorized is an access key AWS would not accept, as opposed
// to a call it accepted and refused for some other reason. Only the
// first one is fixed by entering a new key, which is what the status
// probe reports and what the registry's settings screen exists for.
var errAWSUnauthorized = errors.New("AWS refused this access key")

// awsRejectedTheKey reads an ECR refusal for whether the key was the
// problem. AWS answers 400 for most of these, so the status alone does
// not say; the exception type does, and it is a stable part of the API.
func awsRejectedTheKey(status int, exception string) bool {
	if status == http.StatusForbidden {
		return true
	}
	for _, name := range []string{
		"UnrecognizedClientException",
		"InvalidSignatureException",
		"InvalidClientTokenId",
		"IncompleteSignature",
		"MissingAuthenticationToken",
		"ExpiredTokenException",
		"AccessDeniedException",
		"SignatureDoesNotMatch",
	} {
		if strings.Contains(exception, name) {
			return true
		}
	}
	return false
}
