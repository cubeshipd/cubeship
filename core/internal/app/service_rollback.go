package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cubeship/internal/user"
)

// ErrNotRestorable refuses rolling back to a deployment that cannot be
// run again as it was. The wrapped message says why.
var ErrNotRestorable = errors.New("this deployment cannot be rolled back to")

// Rollback starts a deploy of the version an earlier deployment ran.
//
// It is an ordinary deploy — same lock, same pre-deploy command, same
// health gate and swap — asked for with that deployment's version
// instead of the app's configured one:
//
//   - an image source redeploys the tag that deployment pulled;
//   - a source that builds redeploys that deployment's commit, which
//     reuses the image already built for it when that image is still the
//     one the build left, and builds the commit again when it is not.
//
// The environment is the app's current one, as with any deploy.
func (s *Service) Rollback(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Scoped, *Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, nil, err
	}
	if err := requireSource(caller, Source(a.Source)); err != nil {
		return nil, nil, err
	}
	d, err := s.Repo().DeploymentByID(ctx, a.ID, deploymentID)
	if err != nil {
		return nil, nil, ErrDeploymentNotFound
	}
	live, err := s.liveDeployment(ctx, a)
	if err != nil {
		return nil, nil, err
	}
	d.Live = d.ID == live
	tag, identity, err := rollbackTarget(a, d)
	if err != nil {
		return nil, nil, err
	}
	if a.Status() == StatusStopped {
		if err := s.orch.StartApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	deployment, err := s.orch.StartWithIdentity(ctx, a.ID, tag, identity)
	if err != nil {
		return nil, nil, err
	}
	return a, deployment, nil
}

// rollbackTarget is what a deploy has to be asked for to run d's
// version again, or why it cannot be.
//
// The checks compare d with the app's source as it is now: a deployment
// made before the source was changed names an image or a commit the
// current source would read as something else.
func rollbackTarget(a *Scoped, d *Deployment) (string, DeploymentIdentity, error) {
	refuse := func(reason string) (string, DeploymentIdentity, error) {
		return "", DeploymentIdentity{}, fmt.Errorf("%w: %s", ErrNotRestorable, reason)
	}
	if d.Status != DeploymentSucceeded {
		return refuse("only a deploy that succeeded can be rolled back to")
	}
	if d.Live {
		return refuse("the app is already running this deploy")
	}
	if Source(a.Source).Builds() {
		if d.GitSHA == "" {
			return refuse("this deploy recorded no Git commit")
		}
		return d.GitSHA, DeploymentIdentity{GitSHA: d.GitSHA, GitBranch: d.GitBranch}, nil
	}

	// An image source names its version by tag. A deploy that built, or
	// one pinned by digest, has no tag this source could pull.
	tag := tagOf(d.ImageRef)
	if tag == "" || d.GitSHA != "" || strings.HasPrefix(d.ImageRef, "cubeship-build/") {
		return refuse("this deploy ran an image the app's current source cannot pull by tag")
	}
	repo := strings.TrimSuffix(d.ImageRef, ":"+tag)
	switch Source(a.Source) {
	case SourceExternal:
		if repo != a.SourceImage {
			return refuse(fmt.Sprintf("this deploy ran %s, and the app now pulls %s", repo, a.SourceImage))
		}
	case SourceRegistry:
		if !strings.HasSuffix(repo, "/"+ReferenceOf(a).String()) {
			return refuse("this deploy ran an image from outside this app's registry path")
		}
	}
	return tag, DeploymentIdentity{}, nil
}

// markRestorable fills in which deployments Rollback would accept.
func markRestorable(a *Scoped, history []*Deployment) {
	for _, d := range history {
		_, _, err := rollbackTarget(a, d)
		d.Restorable = err == nil
	}
}
