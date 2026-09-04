package buildkit

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Clone fetches a repository to a temporary directory.
//
// A Dockerfile build needs none of this — BuildKit clones for itself,
// inside the builder. Railpack does: it reads the source to work out how
// to build it, and that reading happens here, in the daemon, before
// BuildKit is involved at all.
//
// go-git rather than the git command, so a Cubeship install stays one
// binary with nothing else to put on the box.
func Clone(ctx context.Context, url, ref string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "cubeship-src-")
	if err != nil {
		return "", nil, fmt.Errorf("create a directory for the source: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	opts := &git.CloneOptions{
		URL: url,
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
		repo, err = cloneThenCheckout(ctx, dir, url, ref)
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone %s: %w", url, err)
	}
	_ = repo
	return dir, cleanup, nil
}

// cloneThenCheckout handles a ref that is not a branch. It gives up the
// shallow fetch: a commit that is not the tip is not in a depth-1 clone,
// and finding out after cloning is worse than fetching more than needed.
func cloneThenCheckout(ctx context.Context, dir, url, ref string) (*git.Repository, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{URL: url})
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
