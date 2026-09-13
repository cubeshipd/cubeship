package templateinstall

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/user"
	"cubeship/template"
)

func (s *Service) run(caller *user.User, rec *Install, p *plan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
	defer cancel()
	if err := s.apply(ctx, caller, rec, p); err != nil {
		s.undo(caller, rec, err)
		return
	}
	now := time.Now()
	rec.Status, rec.Step, rec.FinishedAt = StatusSucceeded, "", &now
	s.save(rec)
}

func (s *Service) save(rec *Install) {
	if err := s.records.Save(context.Background(), rec); err != nil {
		log.Printf("template install %d: record progress: %v", rec.ID, err)
	}
}

// apply creates what the template declares, in the order the instance's
// own dependencies fix: what an app is attached to exists before the
// attachment, and a container is created with the environment it will
// keep, so the deploy comes last.
func (s *Service) apply(ctx context.Context, caller *user.User, rec *Install, p *plan) error {
	step := func(format string, args ...any) {
		rec.Step = fmt.Sprintf(format, args...)
		s.save(rec)
	}
	created := func(kind, name string) {
		rec.Resources = append(rec.Resources, Resource{Kind: kind, Name: name})
		s.save(rec)
	}
	m := p.manifest

	if p.createProject {
		step("Creating project %s", p.project)
		if _, _, err := s.projects.Create(ctx, caller, p.project); err != nil {
			return fmt.Errorf("create project %s: %w", p.project, err)
		}
		created(KindProject, p.project)
	}
	if p.createEnvironment {
		step("Creating environment %s", p.environment)
		if _, err := s.projects.CreateEnvironment(ctx, caller, p.project, p.environment); err != nil {
			return fmt.Errorf("create environment %s: %w", p.environment, err)
		}
		created(KindEnvironment, p.project+"/"+p.environment)
	}

	description := fmt.Sprintf("Installed from the template %s/%s %s", p.owner, p.repo, p.release.Tag)
	for _, db := range m.Databases {
		name := p.databases[db.Key]
		step("Creating database %s", name)
		if _, err := s.datastores.Create(ctx, caller, datastore.Spec{
			Slug: name, Description: description,
			Engine: datastore.Engine(db.Engine), Version: deref(db.Version),
			Username: deref(db.Username), Database: deref(db.Database), Expose: db.Expose,
		}); err != nil {
			return fmt.Errorf("create database %s: %w", name, err)
		}
		created(KindDatabase, name)
	}
	for _, st := range m.Stores {
		name := p.stores[st.Key]
		step("Creating object store %s", name)
		if _, err := s.stores.Create(ctx, caller, objectstore.ManagedSpec{
			Slug: name, Description: description, Version: deref(st.Version),
		}); err != nil {
			return fmt.Errorf("create object store %s: %w", name, err)
		}
		created(KindStore, name)
	}

	refs := map[string]app.Reference{}
	for _, a := range m.Apps {
		ref := app.Reference{Project: p.project, Environment: p.environment, Name: p.apps[a.Key]}
		refs[a.Key] = ref
		step("Creating app %s", ref)
		source, origin := sourceOf(a)
		if _, err := s.apps.Create(ctx, caller, p.project, p.environment, ref.Name, source, origin); err != nil {
			return fmt.Errorf("create app %s: %w", ref, err)
		}
		created(KindApp, ref.String())
		if err := s.configure(ctx, caller, ref, a); err != nil {
			return fmt.Errorf("configure app %s: %w", ref, err)
		}
	}

	for _, db := range m.Databases {
		name := p.databases[db.Key]
		step("Waiting for database %s to start", name)
		if err := s.waitFor(ctx, "database "+name, func() (bool, error) {
			d, err := s.datastores.Resolve(ctx, caller, name, RoleToInstall)
			if err != nil {
				return false, err
			}
			if d.Status == datastore.StatusFailed {
				return false, fmt.Errorf("database %s did not start: %s", name, d.Error)
			}
			return d.Status == datastore.StatusRunning, nil
		}); err != nil {
			return err
		}
	}
	for _, st := range m.Stores {
		name := p.stores[st.Key]
		step("Waiting for object store %s to start", name)
		if err := s.waitFor(ctx, "object store "+name, func() (bool, error) {
			store, err := s.stores.Resolve(ctx, caller, name, RoleToInstall)
			if err != nil {
				return false, err
			}
			if store.Status == objectstore.StatusFailed {
				return false, fmt.Errorf("object store %s did not start", name)
			}
			return store.Status == objectstore.StatusRunning, nil
		}); err != nil {
			return err
		}
		for _, bucket := range st.Buckets {
			if err := s.stores.CreateBucket(ctx, caller, name, bucket); err != nil {
				return fmt.Errorf("create bucket %s in %s: %w", bucket, name, err)
			}
		}
	}

	for _, a := range m.Apps {
		ref := refs[a.Key]
		step("Wiring app %s", ref)
		for _, d := range a.Domains {
			host, err := s.resolve(ctx, caller, p, d.Host)
			if err != nil {
				return fmt.Errorf("app %s domain: %w", ref, err)
			}
			if _, err := s.apps.AddDomain(ctx, caller, ref, host, d.Port); err != nil {
				return fmt.Errorf("add %s to app %s: %w", host, ref, err)
			}
		}
		for _, at := range a.Attach {
			var err error
			if at.Kind == "database" {
				_, err = s.datastores.Attach(ctx, caller, p.databases[at.Key], ref.String(), at.Prefix)
			} else {
				_, err = s.stores.Attach(ctx, caller, p.storeName(at.Key), ref.String(), deref(at.Bucket), at.Prefix)
			}
			if err != nil {
				return fmt.Errorf("attach %s to app %s: %w", at.Key, ref, err)
			}
		}
		if len(a.Env) > 0 {
			env := envvar.Map{}
			for name, value := range a.Env {
				resolved, err := s.resolve(ctx, caller, p, value)
				if err != nil {
					return fmt.Errorf("app %s variable %s: %w", ref, name, err)
				}
				env[name] = resolved
			}
			if _, err := s.apps.MergeEnv(ctx, caller, ref, env, nil); err != nil {
				return fmt.Errorf("set variables on app %s: %w", ref, err)
			}
		}
	}

	for _, a := range m.Apps {
		ref := refs[a.Key]
		step("Deploying app %s", ref)
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

// resolve fills the references in one value from what the install created.
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

// storeName is a store key's name on the instance: one the install
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
			return fmt.Errorf("%s was still not ready when the install ran out of time", what)
		case <-time.After(s.Poll):
		}
	}
}

// undo deletes what an install created, newest first, and records why it
// failed. It carries on past a resource it cannot delete, so one refusal
// does not leave everything behind it standing too.
func (s *Service) undo(caller *user.User, rec *Install, cause error) {
	rec.Step = "Undoing what the install created"
	s.save(rec)
	ctx := context.Background()
	var problems []string
	for i := len(rec.Resources) - 1; i >= 0; i-- {
		r := rec.Resources[i]
		if err := s.remove(ctx, caller, r); err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err))
		}
	}
	now := time.Now()
	rec.Status, rec.Step, rec.FinishedAt = StatusFailed, "", &now
	rec.Error = cause.Error()
	if len(problems) > 0 {
		rec.Error += ". Undoing the install also failed for " + strings.Join(problems, "; ")
	}
	s.save(rec)
}

func (s *Service) remove(ctx context.Context, caller *user.User, r Resource) error {
	var err error
	switch r.Kind {
	case KindApp:
		ref, perr := app.ParseReference(r.Name)
		if perr != nil {
			return perr
		}
		_, err = s.apps.Delete(ctx, caller, ref)
	case KindStore:
		_, err = s.stores.Delete(ctx, caller, r.Name)
	case KindDatabase:
		_, err = s.datastores.Delete(ctx, caller, r.Name)
	case KindEnvironment:
		projectSlug, envSlug, _ := strings.Cut(r.Name, "/")
		_, err = s.projects.DeleteEnvironment(ctx, caller, projectSlug, envSlug)
	case KindProject:
		_, err = s.projects.Delete(ctx, caller, r.Name)
	}
	// Already gone — a project deleted before its environment — is the
	// outcome undoing wants.
	if errors.Is(err, app.ErrNotFound) || errors.Is(err, project.ErrNotFound) ||
		errors.Is(err, project.ErrEnvironmentNotFound) || errors.Is(err, datastore.ErrNotFound) ||
		errors.Is(err, objectstore.ErrNotFound) {
		return nil
	}
	return err
}

// Recover undoes every install a previous run of the daemon left running:
// the worker applying it went with that process, and what it had created
// is half an install nobody asked for.
func (s *Service) Recover(ctx context.Context) error {
	running, err := s.records.Running(ctx)
	if err != nil {
		return err
	}
	for _, rec := range running {
		var caller *user.User
		if rec.CreatedBy != 0 {
			caller, _ = s.users.ByID(ctx, rec.CreatedBy)
		}
		if caller == nil {
			now := time.Now()
			rec.Status, rec.Step, rec.FinishedAt = StatusFailed, "", &now
			rec.Error = "the daemon restarted during the install and the account that started it no longer exists, so what it created was left in place"
			s.save(rec)
			continue
		}
		s.undo(caller, rec, errors.New("the daemon restarted during the install"))
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
