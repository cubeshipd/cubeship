package templateinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/template"
	"cubeship/internal/user"
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
	ctx, cancel := context.WithTimeout(context.Background(), s.timeoutFor(p.manifest))
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

// iconRepository reads the repository id out of a listing's icon address,
// as the catalog writes it or as HTTPCatalog rewrote it to the instance's.
var iconRepository = regexp.MustCompile(`icons/(-?[0-9]{1,20})/[0-9a-f]{7,64}\.png$`)

// projectIcon gives a project the install created the icon of the release
// installed. Only a created one: a project that already existed keeps the
// picture somebody chose. Best effort — a project with no picture wears a
// mark, which is no reason to undo an install.
func (s *Service) projectIcon(ctx context.Context, caller *user.User, p *plan) {
	c, err := s.source()
	if err != nil {
		return
	}
	body, err := c.Template(ctx, p.owner, p.repo)
	if err != nil {
		log.Printf("template install %s/%s: read its icon address: %v", p.owner, p.repo, err)
		return
	}
	var listed struct {
		IconURL *string `json:"icon_url"`
	}
	if err := json.Unmarshal(body, &listed); err != nil || listed.IconURL == nil {
		return
	}
	m := iconRepository.FindStringSubmatch(*listed.IconURL)
	if m == nil {
		return
	}
	icon, err := c.Icon(ctx, m[1], p.release.Commit+".png")
	if err != nil {
		log.Printf("template install %s/%s: fetch its icon: %v", p.owner, p.repo, err)
		return
	}
	if err := s.projects.SetImage(ctx, caller, p.project, bytes.NewReader(icon)); err != nil {
		log.Printf("template install %s/%s: give project %s its icon: %v", p.owner, p.repo, p.project, err)
	}
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
		s.projectIcon(ctx, caller, p)
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
		// The manifest's list, already normalized by the validator. The
		// daemon normalizes it again rather than trusting it — a
		// manifest reaches here from a catalog, not from this process.
		Extensions: db.Extensions,
		Username:   deref(db.Username), Database: deref(db.Database), Expose: db.Expose,
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
	// After configuring, which is where scale and spread are set: a volume
	// and a published port are refused on an app that is not one copy.
	if err := s.addVolumes(ctx, caller, ref, a.Key, a.Volumes, nil); err != nil {
		return err
	}
	return s.addTCPPorts(ctx, caller, p, ref, a.Key, a.TCP, nil)
}

// addVolumes gives an app volumes. An update records each, because the app
// they were added to stays when the update is undone; an install passes no
// recorder, since deleting the app takes them with it.
func (s *Service) addVolumes(ctx context.Context, caller *user.User, ref app.Reference, key string,
	volumes []template.NormalizedVolume, created recorder) error {
	for _, v := range volumes {
		if _, err := s.apps.AddVolume(ctx, caller, ref, v.Path); err != nil {
			return fmt.Errorf("add volume %s to app %s: %w", v.Path, ref, err)
		}
		if created != nil {
			created(KindVolume, key, ref.String()+" "+v.Path)
		}
	}
	return nil
}

// addTCPPorts publishes an app's ports. An update records each, for the
// reason addVolumes does; an install passes no recorder.
func (s *Service) addTCPPorts(ctx context.Context, caller *user.User, p *plan, ref app.Reference, key string,
	ports []template.NormalizedTCP, created recorder) error {
	for _, tp := range ports {
		host, err := tcpHostPort(p, tp)
		if err != nil {
			return fmt.Errorf("app %s port %d: %w", ref, tp.Port, err)
		}
		published, err := s.apps.AddTCPPort(ctx, caller, ref, tp.Port, host)
		if err != nil {
			return fmt.Errorf("publish port %d of app %s: %w", tp.Port, ref, err)
		}
		if created != nil {
			created(KindTCPPort, key, ref.String()+" "+strconv.Itoa(published.ContainerPort))
		}
	}
	return nil
}

// tcpHostPort is the host port a template's entry asks for once its input
// is answered, or 0 for the instance to pick one.
func tcpHostPort(p *plan, tp template.NormalizedTCP) (int, error) {
	if tp.Host == nil {
		return 0, nil
	}
	value, err := template.ReplaceReferences(*tp.Host, func(kind, key, _ string) (string, error) {
		if kind == "input" {
			return p.inputs[key], nil
		}
		return "", errors.New("a host port is a number or an input")
	})
	if err != nil {
		return 0, err
	}
	// A number input's answer can arrive as 2222.0 from a default.
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || n != float64(int(n)) {
		return 0, fmt.Errorf("%q is not a port number", value)
	}
	return int(n), nil
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
