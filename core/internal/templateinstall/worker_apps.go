package templateinstall

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/template"
	"cubeship/internal/user"
)

func (s *Service) deploy(ctx context.Context, caller *user.User, run *Run, ref app.Reference) error {
	s.step(run, "Deploying app %s", ref)
	_, started, err := s.apps.Deploy(ctx, caller, ref, "")
	if err != nil {
		return fmt.Errorf("deploy app %s: %w", ref, err)
	}
	finished, err := s.apps.WaitForDeployment(ctx, caller, ref, started.ID)
	if err != nil {
		return fmt.Errorf("deploy app %s: %w", ref, err)
	}
	if finished.Status != app.DeploymentSucceeded {
		return fmt.Errorf("deploy app %s failed: %s", ref, finished.Error)
	}
	return nil
}

// configure gives an app what the template says about how it runs.
func (s *Service) configure(ctx context.Context, caller *user.User, ref app.Reference, a template.NormalizedApp) error {
	var limits *app.Limits
	if a.Limits != nil {
		l := app.Limits{}
		if a.Limits.CPU != nil {
			l.CPU = *a.Limits.CPU
		}
		if a.Limits.MemoryBytes != nil {
			l.Memory = *a.Limits.MemoryBytes
		}
		limits = &l
	}
	var auto *app.Autoscale
	if a.Autoscale != nil {
		auto = &app.Autoscale{Min: a.Autoscale.Min, Max: a.Autoscale.Max, CPU: a.Autoscale.CPU}
	}
	var place *app.Placement
	if a.Scale != nil || a.Spread {
		place = &app.Placement{}
		if a.Scale != nil {
			place.Replicas = *a.Scale
		}
		if a.Spread {
			spread := true
			place.Spread = &spread
		}
	}
	if a.Health == nil && limits == nil && auto == nil && place == nil {
		return nil
	}
	_, err := s.apps.Update(ctx, caller, ref, nil, nil, a.Health, limits, auto, place)
	return err
}

func sourceOf(a template.NormalizedApp) (app.Source, app.Origin) {
	switch a.Source.Type {
	case "image":
		return app.SourceExternal, app.Origin{Image: a.Source.Image, Tag: deref(a.Source.Tag)}
	case "railpack":
		return app.SourceRailpack, app.Origin{Repo: a.Source.Repo, Ref: deref(a.Source.Ref)}
	default:
		return app.SourceDockerfile, app.Origin{Repo: a.Source.Repo, Ref: deref(a.Source.Ref), Dockerfile: deref(a.Source.Dockerfile)}
	}
}

// resolve fills the references in one value from what exists on the
// instance.
func (s *Service) resolve(ctx context.Context, caller *user.User, p *plan, value string) (string, error) {
	return template.ReplaceReferences(value, func(kind, key, attr string) (string, error) {
		switch kind {
		case "input":
			return p.inputs[key], nil
		case "db":
			creds, err := s.datastores.Credentials(ctx, caller, p.databases[key])
			if err != nil {
				return "", err
			}
			switch attr {
			case "host":
				return creds.InternalHost, nil
			case "port":
				return strconv.Itoa(creds.InternalPort), nil
			case "user":
				return creds.Username, nil
			case "password":
				return creds.Password, nil
			case "name":
				return creds.Database, nil
			}
		case "store":
			creds, err := s.stores.Credentials(ctx, caller, p.storeName(key))
			if err != nil {
				return "", err
			}
			switch attr {
			case "endpoint":
				return creds.Endpoint, nil
			case "bucket":
				return p.bucketFor(key), nil
			}
		case "app":
			for _, a := range p.manifest.Apps {
				if a.Key != key {
					continue
				}
				switch attr {
				case "internal":
					return template.InternalHost(p.project, p.environment, p.apps[key]), nil
				case "port":
					if a.Port != nil {
						return strconv.Itoa(*a.Port), nil
					}
					return strconv.Itoa(app.DefaultPort), nil
				case "host":
					if len(a.Domains) > 0 {
						return s.resolve(ctx, caller, p, a.Domains[0].Host)
					}
					return template.InternalHost(p.project, p.environment, p.apps[key]), nil
				}
			}
		}
		return "", errors.New("this instance has nothing to put there")
	})
}

func (p *plan) ref(key string) app.Reference {
	return app.Reference{Project: p.project, Environment: p.environment, Name: p.apps[key]}
}

func (p *plan) description() string {
	return fmt.Sprintf("Installed from the template %s/%s", p.owner, p.repo)
}

// storeName is a store key's name on the instance: one the installation
// created, or the one an input chose.
func (p *plan) storeName(key string) string {
	if name, ok := p.stores[key]; ok {
		return name
	}
	return p.storeInputs[key]
}

// bucketFor is what ${store.key.bucket} means: the first bucket a created
// store was given, or the bucket an attachment names on a chosen one.
func (p *plan) bucketFor(key string) string {
	for _, st := range p.manifest.Stores {
		if st.Key == key && len(st.Buckets) > 0 {
			return st.Buckets[0]
		}
	}
	for _, a := range p.manifest.Apps {
		for _, at := range a.Attach {
			if at.Kind == "store" && at.Key == key && at.Bucket != nil {
				return *at.Bucket
			}
		}
	}
	return ""
}

func (s *Service) waitFor(ctx context.Context, what string, ready func() (bool, error)) error {
	for {
		done, err := ready()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s was still not ready when the run ran out of time", what)
		case <-time.After(s.Poll):
		}
	}
}

// rollbackUpdate puts an installation back as the update found it: what
// the update created is deleted, what it changed on existing apps is set
// back, and those apps are deployed again as they were.
func (s *Service) rollbackUpdate(caller *user.User, run *Run, cause error) {
	s.step(run, "Putting the installation back as it was")
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
	defer cancel()
	problems := s.removeAll(ctx, caller, run.Created)
	for _, snap := range run.Snapshot {
		ref, err := app.ParseReference(snap.Ref)
		if err != nil {
			problems = append(problems, fmt.Sprintf("app %s: %v", snap.Ref, err))
			continue
		}
		if err := s.restore(ctx, caller, ref, snap); err != nil {
			problems = append(problems, fmt.Sprintf("app %s: %v", ref, err))
			continue
		}
		if err := s.deploy(ctx, caller, run, ref); err != nil {
			problems = append(problems, err.Error())
		}
	}
	s.finish(run, withProblems(cause, problems, "putting it back"))
}

func (s *Service) restore(ctx context.Context, caller *user.User, ref app.Reference, snap AppSnapshot) error {
	if snap.SourceChanged {
		source := app.Source(snap.Source)
		origin := app.Origin{Image: snap.Image, Tag: snap.Tag, Repo: snap.Repo, Ref: snap.GitRef, Dockerfile: snap.Dockerfile}
		if _, err := s.apps.Update(ctx, caller, ref, &source, &origin, nil, nil, nil, nil); err != nil {
			return fmt.Errorf("set the source back: %w", err)
		}
	}
	if snap.SettingsChanged {
		health := snap.Health
		limits := app.Limits{CPU: snap.CPU, Memory: snap.Memory}
		auto := app.Autoscale{Min: snap.AutoscaleMin, Max: snap.AutoscaleMax, CPU: snap.AutoscaleCPU}
		place := app.Placement{Replicas: snap.Scale, Spread: &snap.Spread}
		if _, err := s.apps.Update(ctx, caller, ref, nil, nil, &health, &limits, &auto, &place); err != nil {
			return fmt.Errorf("set the settings back: %w", err)
		}
	}
	if len(snap.Env) > 0 {
		set := envvar.Map{}
		var unset []string
		for name, value := range snap.Env {
			if value == nil {
				unset = append(unset, name)
			} else {
				set[name] = *value
			}
		}
		sort.Strings(unset)
		if _, err := s.apps.MergeEnv(ctx, caller, ref, set, unset); err != nil {
			return fmt.Errorf("set the variables back: %w", err)
		}
	}
	return nil
}

// runUninstall deletes the installation's apps, then its data when asked,
// then the project and environment it created once nothing is left in
// them. Anything it could not delete keeps the installation installed.
func (s *Service) runUninstall(caller *user.User, in *Install, run *Run) {
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
	defer cancel()
	var problems []string
	remove := func(kinds ...string) {
		for i := len(in.Resources) - 1; i >= 0; i-- {
			r := in.Resources[i]
			if !slices.Contains(kinds, r.Kind) {
				continue
			}
			if err := s.remove(ctx, caller, r, !run.KeepData); err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err))
			}
		}
	}

	s.step(run, "Deleting the apps")
	remove(KindApp)
	if !run.KeepData {
		s.step(run, "Deleting the databases and object stores")
		remove(KindStore, KindDatabase)
	}

	s.step(run, "Deleting what is left empty")
	apps, err := s.apps.List(ctx, caller)
	if err != nil {
		problems = append(problems, fmt.Sprintf("list apps: %v", err))
	} else {
		for i := len(in.Resources) - 1; i >= 0; i-- {
			r := in.Resources[i]
			if r.Kind != KindEnvironment && r.Kind != KindProject {
				continue
			}
			inUse := slices.ContainsFunc(apps, func(a *app.Scoped) bool {
				if r.Kind == KindEnvironment {
					return a.ProjectSlug+"/"+a.EnvironmentSlug == r.Name
				}
				return a.ProjectSlug == r.Name
			})
			if inUse {
				continue
			}
			if err := s.remove(ctx, caller, r, false); err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err))
			}
		}
	}

	if len(problems) > 0 {
		s.finish(run, fmt.Errorf("could not delete %s", strings.Join(problems, "; ")))
		return
	}
	in.Status = StatusUninstalled
	s.saveInstall(in)
	s.finish(run, nil)
}

func (s *Service) removeAll(ctx context.Context, caller *user.User, resources []Resource) []string {
	var problems []string
	for i := len(resources) - 1; i >= 0; i-- {
		r := resources[i]
		// Undoing what a run created: its data is only what it wrote.
		if err := s.remove(ctx, caller, r, true); err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err))
		}
	}
	return problems
}

// remove deletes one resource. deleteData says whether an app's volumes'
// data goes with it.
func (s *Service) remove(ctx context.Context, caller *user.User, r Resource, deleteData bool) error {
	var err error
	switch r.Kind {
	case KindApp:
		ref, perr := app.ParseReference(r.Name)
		if perr != nil {
			return perr
		}
		_, err = s.apps.DeleteApp(ctx, caller, ref, deleteData)
	case KindVolume:
		refText, volumePath, _ := strings.Cut(r.Name, " ")
		ref, perr := app.ParseReference(refText)
		if perr != nil {
			return perr
		}
		var a *app.Scoped
		if a, err = s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
			for _, v := range a.Volumes {
				if v.Path == volumePath {
					err = s.apps.RemoveVolume(ctx, caller, ref, v.ID, deleteData)
				}
			}
		}
	case KindTCPPort:
		refText, portText, _ := strings.Cut(r.Name, " ")
		ref, perr := app.ParseReference(refText)
		if perr != nil {
			return perr
		}
		containerPort, perr := strconv.Atoi(portText)
		if perr != nil {
			return fmt.Errorf("cannot read the port %q", r.Name)
		}
		var a *app.Scoped
		if a, err = s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
			for _, tp := range a.TCPPorts {
				if tp.ContainerPort == containerPort {
					err = s.apps.RemoveTCPPort(ctx, caller, ref, tp.ID)
				}
			}
		}
	case KindStore:
		_, err = s.stores.Delete(ctx, caller, r.Name)
	case KindDatabase:
		_, err = s.datastores.Delete(ctx, caller, r.Name)
	case KindEnvironment:
		projectSlug, envSlug, _ := strings.Cut(r.Name, "/")
		_, err = s.projects.DeleteEnvironment(ctx, caller, projectSlug, envSlug)
	case KindProject:
		_, err = s.projects.Delete(ctx, caller, r.Name)
	case KindDomain:
		refText, host, _ := strings.Cut(r.Name, " ")
		ref, perr := app.ParseReference(refText)
		if perr != nil {
			return perr
		}
		var a *app.Scoped
		if a, err = s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
			for _, d := range a.Domains {
				if d.Host == host {
					_, err = s.apps.RemoveDomain(ctx, caller, ref, d.ID)
				}
			}
		}
	case KindAttachment:
		parts := strings.Fields(r.Name)
		if len(parts) < 3 {
			return fmt.Errorf("cannot read the attachment %q", r.Name)
		}
		if parts[1] == "database" {
			_, err = s.datastores.Detach(ctx, caller, parts[2], parts[0])
		} else {
			bucket := ""
			if len(parts) > 3 {
				bucket = parts[3]
			}
			_, err = s.stores.Detach(ctx, caller, parts[2], parts[0], bucket)
		}
	}
	// Already gone — a project deleted before its environment, an app
	// before its domain — is the outcome removing wants.
	if errors.Is(err, app.ErrNotFound) || errors.Is(err, project.ErrNotFound) ||
		errors.Is(err, project.ErrEnvironmentNotFound) || errors.Is(err, datastore.ErrNotFound) ||
		errors.Is(err, objectstore.ErrNotFound) || errors.Is(err, datastore.ErrNotAttached) ||
		errors.Is(err, objectstore.ErrNotAttached) || errors.Is(err, app.ErrDomainNotFound) ||
		errors.Is(err, app.ErrVolumeNotFound) || errors.Is(err, app.ErrTCPPortNotFound) {
		return nil
	}
	return err
}

// Recover finishes what a previous run of the daemon left running: the
// worker went with that process. An install is undone, an update is put
// back, and an uninstall is carried on from wherever it stopped.
func (s *Service) Recover(ctx context.Context) error {
	running, err := s.records.RunningRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range running {
		in, err := s.records.Install(ctx, run.InstallID)
		if err != nil {
			log.Printf("template run %d: read its installation: %v", run.ID, err)
			continue
		}
		var caller *user.User
		if run.CreatedBy != 0 {
			caller, _ = s.users.ByID(ctx, run.CreatedBy)
		}
		if caller == nil {
			if run.Kind == RunInstall {
				in.Status = StatusFailed
				s.saveInstall(in)
			}
			s.finish(run, errors.New("the daemon restarted during the run and the account that started it no longer exists, so nothing was undone"))
			continue
		}
		cause := errors.New("the daemon restarted during the run")
		switch run.Kind {
		case RunInstall:
			s.failInstall(caller, in, run, cause)
		case RunUpdate:
			s.rollbackUpdate(caller, run, cause)
		case RunUninstall:
			s.runUninstall(caller, in, run)
		}
	}
	return nil
}

func deref[T any](v *T) T {
	var zero T
	if v == nil {
		return zero
	}
	return *v
}
