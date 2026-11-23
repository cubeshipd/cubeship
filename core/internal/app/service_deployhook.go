package app

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// DeployHook is an app's deploy webhook: the address a CI posts to when
// it has pushed something, and the token that address carries.
//
// It exists for the registries Cubeship does not run. A push to its own
// registry already deploys, and a push to a connected repository
// already builds; an image on GHCR or Docker Hub had no way to say
// "there is a new one" short of an API key with the run of the account.
// The token reaches one app and does one thing to it.
type DeployHook struct {
	// URL is empty while the instance has no domain: there is no public
	// address to give yet, and a guess would be one a CI cannot reach.
	URL   string
	Token string
}

// HookPath is where the webhook is served, ahead of the token.
const HookPath = "/hooks/deploy/"

// HookRequest is what one webhook call asks for. Both are optional, and
// each belongs to one kind of app — see DeployFromHook.
type HookRequest struct {
	Tag string
	Ref string
	// Wait holds the answer until the deploy has finished.
	Wait bool
}

// HookResult is what a webhook call started: which app, which deploy,
// and the tag or ref it runs — one of the two, by the app's source.
type HookResult struct {
	App        Reference
	Deployment *Deployment
	Tag, Ref   string
}

// ErrRefNotAllowed reports a ref sent to an app that does not build.
var ErrRefNotAllowed = errors.New("only a building app names a ref; an app that runs a published image names a tag")

// ErrInvalidRef reports a ref no Git repository could hold.
var ErrInvalidRef = errors.New("a ref is a branch, tag or commit: no spaces, not starting with a dash, up to 255 characters")

// ValidRef is a light check on a branch, tag or commit: the builder is
// the judge of whether it exists, but a ref that starts with a dash is
// an option to whatever reads it, and a space or a control character is
// never part of one.
func ValidRef(ref string) bool {
	if ref == "" || len(ref) > 255 || strings.HasPrefix(ref, "-") {
		return false
	}
	return !strings.ContainsFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// DeployHook reads an app's webhook. Seeing it is being able to deploy
// with it, so it takes exactly what deploying does: managing the app,
// and for one that builds, the admin role.
func (s *Service) DeployHook(ctx context.Context, caller *user.User, ref Reference) (*DeployHook, error) {
	a, err := s.hookOwner(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	token, err := s.Repo().DeployToken(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	return s.hookFor(ctx, token), nil
}

// RotateDeployHook gives an app a new token. The old one stops working
// with the same statement, so a leaked URL is dead the moment this
// answers.
func (s *Service) RotateDeployHook(ctx context.Context, caller *user.User, ref Reference) (*DeployHook, error) {
	a, err := s.hookOwner(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	token, err := s.Repo().RotateDeployToken(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	return s.hookFor(ctx, token), nil
}

func (s *Service) hookOwner(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := requireSource(caller, Source(a.Source)); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) hookFor(ctx context.Context, token string) *DeployHook {
	hook := &DeployHook{Token: token}
	if host := settings.APIHostFor(s.InstanceConfig(ctx).Domain); host != "" {
		hook.URL = "https://" + host + HookPath + token
	}
	return hook
}

// DeployFromHook deploys the app a token names. Holding the token is the
// authorization: there is no caller, and nobody to ask.
//
// A tag is for an app that runs a published image. On an external app,
// or a registry app pinned to a tag, it is saved — the same edit as
// changing the tag on the settings screen — so the next deploy from
// anywhere stays on it. On a registry app that follows pushes it is
// only deployed: saving it would pin the app, and pinning is how
// autodeploy is turned off.
//
// A ref is for an app that builds, and is this one deploy's, exactly
// as the tag argument of a manual deploy is.
//
// An unknown token is ErrNotFound and nothing else, so the answer says
// nothing about which apps exist.
func (s *Service) DeployFromHook(ctx context.Context, token string, req HookRequest) (*HookResult, error) {
	id, err := s.Repo().AppIDByDeployToken(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a, err := s.Repo().ScopedByID(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	result, err := s.deployFromHook(ctx, a, req)
	if result == nil {
		// Named even when refused, so the audit log can say which app
		// a bad call was for without the token ever being written down.
		result = &HookResult{App: ReferenceOf(a)}
	}
	return result, err
}

func (s *Service) deployFromHook(ctx context.Context, a *Scoped, req HookRequest) (*HookResult, error) {
	source := Source(a.Source)
	tag, ref := strings.TrimSpace(req.Tag), strings.TrimSpace(req.Ref)
	var run string
	switch {
	case source.Builds():
		if tag != "" {
			return nil, ErrTagNotAllowed
		}
		if ref != "" && !ValidRef(ref) {
			return nil, ErrInvalidRef
		}
		run = ref
	case ref != "":
		return nil, ErrRefNotAllowed
	case tag != "":
		if !ValidTag(tag) {
			return nil, ErrInvalidTag
		}
		if source == SourceExternal || a.SourceTag != "" {
			origin := Origin{
				Image: a.SourceImage, Tag: tag,
				Repo: a.SourceRepo, Ref: a.SourceRef, Dockerfile: a.SourceDockerfile,
			}
			if err := checkOrigin(source, &origin); err != nil {
				return nil, err
			}
			if _, err := s.Repo().Update(ctx, a.ID, nil, &origin, nil, nil, nil); err != nil {
				return nil, err
			}
		}
		run = tag
	}

	if a.Status() == StatusStopped {
		if err := s.orch.StartApp(ctx, a.ID); err != nil {
			return nil, err
		}
	}
	d, err := s.orch.Start(ctx, a.ID, run)
	if err != nil {
		return nil, err
	}
	result := &HookResult{App: ReferenceOf(a), Deployment: d}
	if source.Builds() {
		// No ref is the one the app is configured with, and an empty one
		// of those is the repository's default branch.
		result.Ref = cmp.Or(run, a.SourceRef)
	} else {
		// What the orchestrator settled on, before resolving it into a
		// full image reference overwrites the row.
		result.Tag = d.ImageRef
	}
	if req.Wait {
		// A wait that runs out hands back where the deploy had got to,
		// with the context's error; the deploy itself carries on.
		done, err := s.orch.WaitFor(ctx, a.ID, d.ID)
		if done != nil {
			result.Deployment = done
		}
		return result, err
	}
	return result, nil
}
