package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/project"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Create registers an app in a project's environment and returns it,
// including the registry path a push should target.
func (s *Service) Create(ctx context.Context, caller *user.User, projectSlug, envSlug, name string, source Source, origin Origin) (*Scoped, error) {
	return s.CreateWithPredeploy(ctx, caller, projectSlug, envSlug, name, source, origin, nil)
}

func (s *Service) CreateWithPredeploy(ctx context.Context, caller *user.User, projectSlug, envSlug, name string, source Source, origin Origin, predeploy *Predeploy) (*Scoped, error) {
	if envSlug == "" {
		envSlug = project.ProductionEnvSlug
	}
	if source == "" {
		source = DefaultSource
	}
	if !source.Valid() {
		return nil, ErrUnknownSource
	}
	if predeploy != nil && !predeploy.Valid() {
		return nil, ErrInvalidPredeploy
	}
	if err := checkOrigin(source, &origin); err != nil {
		return nil, err
	}
	// The name becomes the last path component of the app's registry
	// image reference, so it is checked before anything is looked up.
	if slug.Reserved(name) {
		return nil, slug.ErrReserved
	}
	if !slug.Valid(name) {
		return nil, slug.ErrInvalid
	}

	// Creating an app that builds is deciding that this instance will
	// execute whatever that source contains, so it takes the same role
	// deploying it does. A member creating one they could never deploy
	// would be an odd thing to allow.
	if err := allowApps(caller, projectSlug, user.LevelManage); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, project.ErrNotFound
		}
		return nil, err
	}
	if err := requireSource(caller, source); err != nil {
		return nil, err
	}
	p, err := s.projects.Repo().BySlug(ctx, projectSlug)
	if err != nil {
		return nil, project.ErrNotFound
	}
	env, err := s.projects.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, project.ErrEnvironmentNotFound
	}
	ref := Reference{Project: p.Slug, Environment: env.Slug, Name: name}
	created, err := s.Repo().Create(ctx, p.ID, env.ID, name, source, origin)
	if err != nil {
		// The unique index is the authority, not a preceding lookup:
		// two concurrent creates of the same name would both pass a
		// check and the loser would surface as a 500.
		if database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	if predeploy != nil {
		if _, err := s.Repo().Update(ctx, created.ID, nil, nil, nil, nil, nil, predeploy); err != nil {
			return nil, err
		}
	}
	return s.Repo().ScopedByReference(ctx, ref.Project, ref.Environment, ref.Name)
}

// Delete removes an app: its container first, then its rows. The order
// matters — a row deleted while the container runs leaves something
// serving traffic that nothing knows how to stop.
//
// Images already pushed stay in the registry. Reclaiming them needs a
// registry garbage collection pass, which is a separate operation.
// Update reconfigures an app: where its image comes
// from, how Traefik decides it is healthy, and which machines run it.
//
// An app is created with almost none of that, so this is where it
// becomes deployable. Changing the source to one that builds is the same
// decision as creating one that builds — this instance will execute
// whatever that repository contains — so it takes the same role, checked
// against the source being moved to rather than the one being left.
func (s *Service) Update(ctx context.Context, caller *user.User, ref Reference, source *Source, origin *Origin, health *string, limits *Limits, auto *Autoscale, place *Placement) (*Scoped, error) {
	return s.UpdateWithPredeploy(ctx, caller, ref, source, origin, health, limits, auto, place, nil)
}

func (s *Service) UpdateStopPolicy(ctx context.Context, caller *user.User, ref Reference, grace time.Duration, signal string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if !ValidStopPolicy(grace, signal) {
		return nil, ErrInvalidStopPolicy
	}
	seconds := int64(grace / time.Second)
	if _, err := s.Repo().UpdateStopPolicy(ctx, a.ID, &seconds, &signal); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

func (s *Service) UpdatePathFilters(ctx context.Context, caller *user.User, ref Reference, watch, ignore []string) (*Scoped, error) {
	if watch == nil {
		watch = []string{}
	}
	if ignore == nil {
		ignore = []string{}
	}
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if !ValidPathFilters(watch, ignore) {
		return nil, ErrInvalidPathFilters
	}
	if _, err := s.Repo().UpdatePathFilters(ctx, a.ID, watch, ignore); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

func (s *Service) UpdateBuildArgs(ctx context.Context, caller *user.User, ref Reference, args map[string]string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	for key := range args {
		if key == "" || strings.HasPrefix(key, "CUBESHIP_") || strings.ContainsAny(key, "= \t\n") {
			return nil, errors.New("invalid or reserved build argument")
		}
	}
	if Source(a.Source) != SourceDockerfile {
		return nil, errors.New("build arguments require a dockerfile source")
	}
	if _, err := s.Repo().UpdateBuildArgs(ctx, a.ID, args); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

// ConfigureNetworking replaces the app's declared container ports and
// readiness checks. Every check must pass before a new container is ready.
func (s *Service) ConfigureNetworking(ctx context.Context, caller *user.User, ref Reference, ports []int, checks []HealthCheck) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	checks, err = validateNetworking(ports, checks)
	if err != nil {
		return nil, err
	}
	if ports == nil {
		ports = []int{}
	}
	if _, err := s.Repo().UpdateNetworking(ctx, a.ID, ports, checks); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

func (s *Service) UpdateWithPredeploy(ctx context.Context, caller *user.User, ref Reference, source *Source, origin *Origin, health *string, limits *Limits, auto *Autoscale, place *Placement, predeploy *Predeploy) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}

	// The source and its origin fields are one decision: checkOrigin
	// judges them together, and an app that names an image its source
	// ignores is exactly what it exists to refuse.
	if source != nil || origin != nil {
		next := Source(a.Source)
		if source != nil {
			next = *source
		}
		if !next.Valid() {
			return nil, ErrUnknownSource
		}
		o := Origin{
			Image: a.SourceImage, Tag: a.SourceTag,
			Repo: a.SourceRepo, Ref: a.SourceRef, Dockerfile: a.SourceDockerfile,
		}
		if origin != nil {
			o = *origin
		}
		if err := checkOrigin(next, &o); err != nil {
			return nil, err
		}
		if err := requireSource(caller, next); err != nil {
			return nil, err
		}
		source, origin = &next, &o
	}

	// The health check path, which is neither a source decision nor a
	// placement one: it is how Traefik decides a container behind a
	// name is worth traffic. Checked rather than trusted because it is
	// interpolated into a dynamic YAML document and a container label —
	// see ValidHealthPath.
	if health != nil && !ValidHealthPath(*health) {
		return nil, ErrInvalidHealthPath
	}
	var legacyHealthChecks []HealthCheck
	legacyHealthPorts := slices.Clone(a.InternalPorts)
	if health != nil {
		legacyHealthChecks = slices.Clone(a.HealthChecks)
		switch {
		case *health == "":
			legacyHealthChecks = []HealthCheck{}
		case len(legacyHealthChecks) > 0:
			legacyHealthChecks[0].Path = *health
		default:
			port := healthPort(a)
			legacyHealthChecks = []HealthCheck{{Path: *health, Port: port}}
			if !slices.Contains(legacyHealthPorts, port) {
				legacyHealthPorts = append(legacyHealthPorts, port)
			}
		}
		legacyHealthChecks, err = validateNetworking(legacyHealthPorts, legacyHealthChecks)
		if err != nil {
			return nil, err
		}
	}

	// The ceiling, checked here because a number the Engine refuses
	// would otherwise be found out one container at a time, on whatever
	// machine tried it, minutes after somebody typed it.
	if limits != nil && !limits.Valid() {
		return nil, ErrInvalidLimits
	}

	// And the rule, refused here for the same reason: one without a
	// ceiling is a loop of requests turning into a loop of replicas
	// until the machine has nothing left, which is a worse outage than
	// the one autoscaling was turned on to avoid.
	if auto != nil && !auto.Valid() {
		return nil, ErrInvalidAutoscale
	}
	if predeploy != nil && !predeploy.Valid() {
		return nil, ErrInvalidPredeploy
	}

	// A volume's data is on one machine and cannot be shared by two
	// copies, so neither where the app runs nor how many of it may change.
	if len(a.Volumes) > 0 {
		if auto != nil && auto.On() {
			return nil, ErrVolumePinsApp
		}
		if place != nil && !keepsVolume(a, *place) {
			return nil, ErrVolumePinsApp
		}
	}
	// A published port is bound on the control plane by one container, so
	// the same two things are refused for the same reason.
	if len(a.TCPPorts) > 0 {
		if auto != nil && auto.On() {
			return nil, ErrTCPPinsApp
		}
		if place != nil && !keepsTCP(a, *place) {
			return nil, ErrTCPPinsApp
		}
	}

	if _, err := s.Repo().Update(ctx, a.ID, source, origin, health, limits, auto, predeploy); err != nil {
		return nil, err
	}
	if health != nil {
		if _, err := s.Repo().UpdateNetworking(ctx, a.ID, legacyHealthPorts, legacyHealthChecks); err != nil {
			return nil, err
		}
	}

	// A ceiling is the one thing about a container the Engine can
	// change while it runs, so it takes effect now rather than on the
	// next deploy. Every other setting on this screen is baked into a
	// container at create time and waits for one.
	if limits != nil {
		if err := s.orch.cap(ctx, &a.App, *limits); err != nil {
			return nil, err
		}
	}

	// Where it runs and where its traffic arrives, which are a
	// different kind of change from the rest of this and are checked
	// together: the machine that serves an app has to be one of the
	// machines running it, or its edge would balance across a set it is
	// not in.
	if place != nil {
		next := Source(a.Source)
		if source != nil {
			next = *source
		}
		if err := s.replace(ctx, a, *place, next); err != nil {
			return nil, err
		}
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}
