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
	"cubeship/internal/user"
	"cubeship/template"
)

func (s *Service) saveInstall(in *Install) {
	if err := s.records.SaveInstall(context.Background(), in); err != nil {
		log.Printf("template installation %d: record it: %v", in.ID, err)
	}
}

func (s *Service) saveRun(run *Run) {
	if err := s.records.SaveRun(context.Background(), run); err != nil {
		log.Printf("template run %d: record progress: %v", run.ID, err)
	}
}

// finish records how a run ended.
func (s *Service) finish(run *Run, err error) {
	now := time.Now()
	run.Step, run.FinishedAt = "", &now
	run.Status = RunSucceeded
	if err != nil {
		run.Status, run.Error = RunFailed, err.Error()
	}
	s.saveRun(run)
}

func (s *Service) step(run *Run, format string, args ...any) {
	run.Step = fmt.Sprintf(format, args...)
	s.saveRun(run)
}

func (s *Service) runInstall(caller *user.User, in *Install, run *Run, p *plan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
	defer cancel()
	if err := s.apply(ctx, caller, in, run, p); err != nil {
		s.failInstall(caller, in, run, err)
		return
	}
	in.Status = StatusInstalled
	s.saveInstall(in)
	s.finish(run, nil)
}

// failInstall deletes what an install created, newest first, and records
// why. It carries on past a resource it cannot delete, so one refusal does
// not leave everything behind it standing too.
func (s *Service) failInstall(caller *user.User, in *Install, run *Run, cause error) {
	s.step(run, "Undoing what the install created")
	problems := s.removeAll(context.Background(), caller, run.Created)
	in.Status = StatusFailed
	s.saveInstall(in)
	s.finish(run, withProblems(cause, problems, "undoing the install"))
}

func withProblems(cause error, problems []string, what string) error {
	if len(problems) == 0 {
		return cause
	}
	return fmt.Errorf("%w. And %s failed for %s", cause, what, strings.Join(problems, "; "))
}

// apply creates what the template declares, in the order the instance's
// own dependencies fix: what an app is attached to exists before the
// attachment, and a container is created with the environment it will
// keep, so the deploy comes last.
func (s *Service) apply(ctx context.Context, caller *user.User, in *Install, run *Run, p *plan) error {
	created := func(kind, key, name string) {
		r := Resource{Kind: kind, Key: key, Name: name}
		run.Created = append(run.Created, r)
		in.Resources = append(in.Resources, r)
		s.saveRun(run)
		s.saveInstall(in)
	}
	m := p.manifest

	if p.createProject {
		s.step(run, "Creating project %s", p.project)
		if _, _, err := s.projects.Create(ctx, caller, p.project); err != nil {
			return fmt.Errorf("create project %s: %w", p.project, err)
		}
		created(KindProject, "", p.project)
	}
	if p.createEnvironment {
		s.step(run, "Creating environment %s", p.environment)
		if _, err := s.projects.CreateEnvironment(ctx, caller, p.project, p.environment); err != nil {
			return fmt.Errorf("create environment %s: %w", p.environment, err)
		}
		created(KindEnvironment, "", p.project+"/"+p.environment)
	}

	for _, db := range m.Databases {
		if err := s.createDatabase(ctx, caller, run, p, db, created); err != nil {
			return err
		}
	}
	for _, st := range m.Stores {
		if err := s.createStore(ctx, caller, run, p, st, created); err != nil {
			return err
		}
	}
	for _, a := range m.Apps {
		if err := s.createApp(ctx, caller, run, p, a, created); err != nil {
			return err
		}
	}

	for _, db := range m.Databases {
		if err := s.waitDatabase(ctx, caller, run, p.databases[db.Key]); err != nil {
			return err
		}
	}
	for _, st := range m.Stores {
		if err := s.waitStore(ctx, caller, run, p.stores[st.Key]); err != nil {
			return err
		}
		for _, bucket := range st.Buckets {
			if err := s.stores.CreateBucket(ctx, caller, p.stores[st.Key], bucket); err != nil {
				return fmt.Errorf("create bucket %s in %s: %w", bucket, p.stores[st.Key], err)
			}
		}
	}

	for _, a := range m.Apps {
		if err := s.wire(ctx, caller, run, p, a, a.Domains, a.Attach, envNames(a), nil); err != nil {
			return err
		}
	}
	for _, a := range m.Apps {
		if err := s.deploy(ctx, caller, run, p.ref(a.Key)); err != nil {
			return err
		}
	}
	return nil
}

type recorder func(kind, key, name string)

func (s *Service) createDatabase(ctx context.Context, caller *user.User, run *Run, p *plan, db template.NormalizedDatabase, created recorder) error {
	name := p.databases[db.Key]
	s.step(run, "Creating database %s", name)
	if _, err := s.datastores.Create(ctx, caller, datastore.Spec{
		Slug: name, Description: p.description(),
		Engine: datastore.Engine(db.Engine), Version: deref(db.Version),
		Username: deref(db.Username), Database: deref(db.Database), Expose: db.Expose,
	}); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	created(KindDatabase, db.Key, name)
	return nil
}

func (s *Service) createStore(ctx context.Context, caller *user.User, run *Run, p *plan, st template.NormalizedStore, created recorder) error {
	name := p.stores[st.Key]
	s.step(run, "Creating object store %s", name)
	if _, err := s.stores.Create(ctx, caller, objectstore.ManagedSpec{
		Slug: name, Description: p.description(), Version: deref(st.Version),
	}); err != nil {
		return fmt.Errorf("create object store %s: %w", name, err)
	}
	created(KindStore, st.Key, name)
	return nil
}

func (s *Service) createApp(ctx context.Context, caller *user.User, run *Run, p *plan, a template.NormalizedApp, created recorder) error {
	ref := p.ref(a.Key)
	s.step(run, "Creating app %s", ref)
	source, origin := sourceOf(a)
	if _, err := s.apps.Create(ctx, caller, p.project, p.environment, ref.Name, source, origin); err != nil {
		return fmt.Errorf("create app %s: %w", ref, err)
	}
	created(KindApp, a.Key, ref.String())
	if err := s.configure(ctx, caller, ref, a); err != nil {
		return fmt.Errorf("configure app %s: %w", ref, err)
	}
	return nil
}

func (s *Service) waitDatabase(ctx context.Context, caller *user.User, run *Run, name string) error {
	s.step(run, "Waiting for database %s to start", name)
	return s.waitFor(ctx, "database "+name, func() (bool, error) {
		d, err := s.datastores.Resolve(ctx, caller, name, RoleToInstall)
		if err != nil {
			return false, err
		}
		if d.Status == datastore.StatusFailed {
			return false, fmt.Errorf("database %s did not start: %s", name, d.Error)
		}
		return d.Status == datastore.StatusRunning, nil
	})
}

func (s *Service) waitStore(ctx context.Context, caller *user.User, run *Run, name string) error {
	s.step(run, "Waiting for object store %s to start", name)
	return s.waitFor(ctx, "object store "+name, func() (bool, error) {
		store, err := s.stores.Resolve(ctx, caller, name, RoleToInstall)
		if err != nil {
			return false, err
		}
		if store.Status == objectstore.StatusFailed {
			return false, fmt.Errorf("object store %s did not start", name)
		}
		return store.Status == objectstore.StatusRunning, nil
	})
}

func envNames(a template.NormalizedApp) []string {
	names := make([]string, 0, len(a.Env))
	for name := range a.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// wire gives an app the domains, attachments and variables named. An
// update records each domain and attachment it adds, because the app they
// were added to stays when the update is undone; an install passes no
// recorder, since deleting the app takes them with it.
func (s *Service) wire(ctx context.Context, caller *user.User, run *Run, p *plan, a template.NormalizedApp,
	domains []template.NormalizedDomain, attach []template.NormalizedAttach, env []string, created recorder) error {
	ref := p.ref(a.Key)
	if len(domains) == 0 && len(attach) == 0 && len(env) == 0 {
		return nil
	}
	s.step(run, "Wiring app %s", ref)
	for _, d := range domains {
		host, err := s.resolve(ctx, caller, p, d.Host)
		if err != nil {
			return fmt.Errorf("app %s domain: %w", ref, err)
		}
		if _, err := s.apps.AddDomain(ctx, caller, ref, host, d.Port); err != nil {
			return fmt.Errorf("add %s to app %s: %w", host, ref, err)
		}
		if created != nil {
			created(KindDomain, a.Key, ref.String()+" "+host)
		}
	}
	for _, at := range attach {
		var err error
		var name string
		if at.Kind == "database" {
			_, err = s.datastores.Attach(ctx, caller, p.databases[at.Key], ref.String(), at.Prefix)
			name = ref.String() + " database " + p.databases[at.Key]
		} else {
			_, err = s.stores.Attach(ctx, caller, p.storeName(at.Key), ref.String(), deref(at.Bucket), at.Prefix)
			name = ref.String() + " store " + p.storeName(at.Key) + " " + deref(at.Bucket)
		}
		if err != nil {
			return fmt.Errorf("attach %s to app %s: %w", at.Key, ref, err)
		}
		if created != nil {
			created(KindAttachment, a.Key, name)
		}
	}
	if len(env) > 0 {
		set := envvar.Map{}
		for _, name := range env {
			resolved, err := s.resolve(ctx, caller, p, a.Env[name])
			if err != nil {
				return fmt.Errorf("app %s variable %s: %w", ref, name, err)
			}
			set[name] = resolved
		}
		if _, err := s.apps.MergeEnv(ctx, caller, ref, set, nil); err != nil {
			return fmt.Errorf("set variables on app %s: %w", ref, err)
		}
	}
	return nil
}

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

func (s *Service) runUpdate(caller *user.User, in *Install, run *Run, up *updatePlan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
	defer cancel()
	if err := s.applyUpdate(ctx, caller, run, up); err != nil {
		s.rollbackUpdate(caller, run, err)
		return
	}
	in.Release, in.Commit, in.Manifest = up.release.Tag, up.release.Commit, up.manifest
	in.Answers = up.answers()
	in.Resources = append(in.Resources, run.Created...)
	s.saveInstall(in)
	s.finish(run, nil)
}

// applyUpdate records every app it is about to change as it is, then
// creates what the release adds, changes what it changed, and deploys
// every app that is new or different.
func (s *Service) applyUpdate(ctx context.Context, caller *user.User, run *Run, up *updatePlan) error {
	created := func(kind, key, name string) {
		run.Created = append(run.Created, Resource{Kind: kind, Key: key, Name: name})
		s.saveRun(run)
	}
	m := up.manifest

	changed := make([]string, 0, len(up.changed))
	for key := range up.changed {
		changed = append(changed, key)
	}
	sort.Strings(changed)

	s.step(run, "Recording the apps as they are")
	for _, key := range changed {
		ch, ref := up.changed[key], up.ref(key)
		a, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall)
		if err != nil {
			return fmt.Errorf("read app %s: %w", ref, err)
		}
		own, _, err := s.apps.Env(ctx, caller, ref)
		if err != nil {
			return fmt.Errorf("read the variables of app %s: %w", ref, err)
		}
		snap := AppSnapshot{
			Ref: ref.String(), SourceChanged: ch.source, SettingsChanged: ch.settings,
			Source: a.Source, Image: a.SourceImage, Tag: a.SourceTag, Repo: a.SourceRepo,
			GitRef: a.SourceRef, Dockerfile: a.SourceDockerfile, Health: a.HealthPath,
			CPU: a.Limits.CPU, Memory: a.Limits.Memory,
			AutoscaleMin: a.Autoscale.Min, AutoscaleMax: a.Autoscale.Max, AutoscaleCPU: a.Autoscale.CPU,
			Scale: a.Scale, Spread: a.Spread, Env: map[string]*string{},
		}
		for _, name := range ch.env {
			if value, ok := own[name]; ok {
				snap.Env[name] = &value
			} else {
				snap.Env[name] = nil
			}
		}
		run.Snapshot = append(run.Snapshot, snap)
	}
	s.saveRun(run)

	for _, db := range m.Databases {
		if up.newDatabases[db.Key] {
			if err := s.createDatabase(ctx, caller, run, up.plan, db, created); err != nil {
				return err
			}
		}
	}
	for _, st := range m.Stores {
		if up.newStores[st.Key] {
			if err := s.createStore(ctx, caller, run, up.plan, st, created); err != nil {
				return err
			}
		}
	}
	for _, a := range m.Apps {
		if up.newApps[a.Key] {
			if err := s.createApp(ctx, caller, run, up.plan, a, created); err != nil {
				return err
			}
		}
	}
	for _, key := range changed {
		ch, ref, a := up.changed[key], up.ref(key), up.app(key)
		s.step(run, "Changing app %s", ref)
		if ch.source {
			source, origin := sourceOf(a)
			if _, err := s.apps.Update(ctx, caller, ref, &source, &origin, nil, nil, nil, nil); err != nil {
				return fmt.Errorf("change the source of app %s: %w", ref, err)
			}
		}
		if ch.settings {
			if err := s.configure(ctx, caller, ref, a); err != nil {
				return fmt.Errorf("configure app %s: %w", ref, err)
			}
		}
	}

	for _, db := range m.Databases {
		if up.newDatabases[db.Key] {
			if err := s.waitDatabase(ctx, caller, run, up.databases[db.Key]); err != nil {
				return err
			}
		}
	}
	for _, st := range m.Stores {
		buckets := up.newBuckets[st.Key]
		if up.newStores[st.Key] {
			if err := s.waitStore(ctx, caller, run, up.stores[st.Key]); err != nil {
				return err
			}
			buckets = st.Buckets
		}
		for _, bucket := range buckets {
			if err := s.stores.CreateBucket(ctx, caller, up.stores[st.Key], bucket); err != nil {
				return fmt.Errorf("create bucket %s in %s: %w", bucket, up.stores[st.Key], err)
			}
		}
	}

	var redeploy []app.Reference
	for _, a := range m.Apps {
		var err error
		switch ch := up.changed[a.Key]; {
		case up.newApps[a.Key]:
			err = s.wire(ctx, caller, run, up.plan, a, a.Domains, a.Attach, envNames(a), created)
		case ch != nil:
			err = s.wire(ctx, caller, run, up.plan, a, ch.domains, ch.attach, ch.env, created)
		default:
			continue
		}
		if err != nil {
			return err
		}
		redeploy = append(redeploy, up.ref(a.Key))
	}
	for _, ref := range redeploy {
		if err := s.deploy(ctx, caller, run, ref); err != nil {
			return err
		}
	}
	return nil
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
			if err := s.remove(ctx, caller, r); err != nil {
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
			if err := s.remove(ctx, caller, r); err != nil {
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
		if err := s.remove(ctx, caller, r); err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", r.Kind, r.Name, err))
		}
	}
	return problems
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
		errors.Is(err, objectstore.ErrNotAttached) || errors.Is(err, app.ErrDomainNotFound) {
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
