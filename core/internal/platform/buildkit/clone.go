package buildkit

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// ResolveCommit reads the current commit and ref of a branch, tag or
// default HEAD without cloning. It uses the same credentials as Clone.
func ResolveCommit(ctx context.Context, url, ref, username, token string) (sha, resolvedRef string, err error) {
	if plumbing.IsHash(ref) {
		return ref, "", nil
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: basicAuth(username, token), PeelingOption: git.AppendPeeled})
	if err != nil {
		return "", "", fmt.Errorf("read Git refs from %s: %w", url, err)
	}
	name := plumbing.HEAD
	if ref != "" {
		name = plumbing.NewBranchReferenceName(ref)
	}
	find := func(name plumbing.ReferenceName) *plumbing.Reference {
		for _, candidate := range refs {
			if candidate.Name() == name {
				return candidate
			}
		}
		return nil
	}
	selected := find(name)
	if selected == nil && ref != "" {
		selected = find(plumbing.ReferenceName(plumbing.NewTagReferenceName(ref).String() + "^{}"))
		if selected == nil {
			selected = find(plumbing.NewTagReferenceName(ref))
		}
	}
	if selected == nil {
		return "", "", fmt.Errorf("no Git branch or tag called %q in %s", ref, url)
	}
	if selected.Type() == plumbing.SymbolicReference {
		if ref == "" {
			resolvedRef = strings.TrimPrefix(selected.Target().String(), "refs/heads/")
		}
		selected = find(selected.Target())
	}
	if selected == nil || selected.Hash().IsZero() {
		return "", "", fmt.Errorf("Git ref %q in %s has no commit", ref, url)
	}
	if resolvedRef == "" {
		resolvedRef = ref
	}
	return selected.Hash().String(), resolvedRef, nil
}

// CommitOf reports the checked-out commit in a cloned source directory.
func CommitOf(dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("open cloned repository: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("read cloned commit: %w", err)
	}
	return head.Hash().String(), nil
}

// Clone fetches a repository to a temporary directory.
//
// A Dockerfile build needs none of this — BuildKit clones for itself,
// inside the builder. Railpack does: it reads the source to work out how
// to build it, and that reading happens here, in the daemon, before
// BuildKit is involved at all.
//
// go-git rather than the git command, so a Cubeship install stays one
// binary with nothing else to put on the box.
//
// Empty credentials make an anonymous clone for public repositories.
func Clone(ctx context.Context, url, ref, username, token string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "cubeship-src-")
	if err != nil {
		return "", nil, fmt.Errorf("create a directory for the source: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	opts := &git.CloneOptions{
		URL:  url,
		Auth: basicAuth(username, token),
		// One commit is all a build needs, and a large repository's
		// history is the slowest part of fetching it.
		Depth:             1,
		SingleBranch:      true,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		Tags:              git.NoTags,
	}
	if ref != "" {
		// A ref can be a branch, a tag or a commit, and only the first
		// two can be asked for by name at clone time.
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, opts)
	if err != nil && ref != "" {
		// Not a branch. Tags and commits both need the clone to happen
		// first and the checkout after.
		repo, err = cloneThenCheckout(ctx, dir, url, ref, username, token)
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone %s: %w", url, err)
	}
	_ = repo
	return dir, cleanup, nil
}

// No token means an anonymous clone.
func basicAuth(username, token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	if username == "" {
		username = "x-access-token"
	}
	return &githttp.BasicAuth{Username: username, Password: token}
}

// cloneThenCheckout handles a ref that is not a branch. It gives up the
// shallow fetch: a commit that is not the tip is not in a depth-1 clone,
// and finding out after cloning is worse than fetching more than needed.
func cloneThenCheckout(ctx context.Context, dir, url, ref, username, token string) (*git.Repository, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:  url,
		Auth: basicAuth(username, token),
	})
	if err != nil {
		return nil, err
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("no branch, tag or commit called %q: %w", ref, err)
	}
	tree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	if err := tree.Checkout(&git.CheckoutOptions{Hash: *hash}); err != nil {
		return nil, fmt.Errorf("check out %s: %w", ref, err)
	}
	return repo, nil
}
