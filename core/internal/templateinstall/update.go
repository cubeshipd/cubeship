package templateinstall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"cubeship/internal/datastore"
	"cubeship/internal/objectstore"
	"cubeship/internal/template"
	"cubeship/internal/user"
)

// What a change in a preview does.
const (
	ActionCreate = "create"
	ActionChange = "change"
	// ActionRemove is a domain the template no longer gives an app: the
	// one thing an update takes away, because it holds no data.
	ActionRemove = "remove"
	// ActionKeep is something the release no longer has, or cannot
	// change: it is left as it is.
	ActionKeep = "keep"
)

// Change is one line of a preview.
type Change struct {
	Action string `json:"action"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// Preview is what bringing an installation in line with the template the
// catalog lists now would do.
type Preview struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Available is the template differing from the one installed. The
	// catalog's commit moves with every template it publishes, so it is the
	// manifests that are compared, never the commits.
	Available bool     `json:"available"`
	Changes   []Change `json:"changes"`
	// Inputs are questions the release asks that the installation has no
	// answer for, including a secret it can no longer read back.
	Inputs []template.NormalizedInput `json:"inputs"`
}

// UpdateRequest is what an update is asked to do.
type UpdateRequest struct {
	// Inputs answers the questions the preview listed.
	Inputs map[string]string
}

type appChange struct {
	source   bool
	settings bool
	env      []string
	domains  []template.NormalizedDomain
	// dropped is the domains the template no longer gives the app. A domain
	// holds no data, and a template that moves one to another app needs it
	// free first.
	dropped []template.NormalizedDomain
	attach  []template.NormalizedAttach
	volumes []template.NormalizedVolume
	tcp     []template.NormalizedTCP
}

func (c *appChange) empty() bool {
	return !c.source && !c.settings && len(c.env) == 0 && len(c.domains) == 0 && len(c.dropped) == 0 &&
		len(c.attach) == 0 && len(c.volumes) == 0 && len(c.tcp) == 0
}

// updatePlan is a release compared with what is installed.
type updatePlan struct {
	*plan
	install *Install
	old     *template.Normalized

	changes []Change
	missing []template.NormalizedInput

	newDatabases map[string]bool
	newStores    map[string]bool
	newApps      map[string]bool
	// newBuckets is the buckets the release adds to a store that exists.
	newBuckets map[string][]string
	// changed is every existing app the release changes, by key.
	changed map[string]*appChange
	// available is the template differing from the one installed.
	available bool
}

// unpinned is m with every source ref pinned to a catalog commit — what
// pinMonorepoSources writes — put back to one placeholder, so two
// publications of the same file compare equal. A change to a Dockerfile
// alone is not seen: the file names the same directory either way.
func unpinned(m *template.Normalized) *template.Normalized {
	out := *m
	out.Apps = slices.Clone(m.Apps)
	for i := range out.Apps {
		out.Apps[i].Source = unpinnedSource(out.Apps[i].Source)
	}
	return &out
}

func unpinnedSource(src template.NormalizedSource) template.NormalizedSource {
	if src.Ref != nil {
		if commit, dir, ok := strings.Cut(*src.Ref, ":"); ok && commitSHA.MatchString(commit) {
			plain := "pinned:" + dir
			src.Ref = &plain
		}
	}
	return src
}

func (up *updatePlan) change(action, kind, name, detail string) {
	up.changes = append(up.changes, Change{Action: action, Kind: kind, Name: name, Detail: detail})
}

func (up *updatePlan) app(key string) template.NormalizedApp {
	for _, a := range up.manifest.Apps {
		if a.Key == key {
			return a
		}
	}
	return template.NormalizedApp{}
}

// PreviewUpdate says what updating an installation to the template the
// catalog lists now would do, and creates and changes nothing.
func (s *Service) PreviewUpdate(ctx context.Context, caller *user.User, id int64) (*Preview, error) {
	up, err := s.planUpdate(ctx, caller, id, nil)
	if err != nil {
		return nil, err
	}
	changes, inputs := up.changes, up.missing
	if changes == nil {
		changes = []Change{}
	}
	if inputs == nil {
		inputs = []template.NormalizedInput{}
	}
	return &Preview{
		From: up.install.Release, To: up.release.Tag, Available: up.available,
		Changes: changes, Inputs: inputs,
	}, nil
}

// Update applies the template the catalog lists now to an installation in
// the background. Secrets it adds, and the instance generated, come back
// here once.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, req UpdateRequest) (*Run, map[string]string, error) {
	up, err := s.planUpdate(ctx, caller, id, req.Inputs)
	if err != nil {
		return nil, nil, err
	}
	if !up.available {
		return nil, nil, ErrUpToDate
	}
	if len(up.missing) > 0 {
		in := up.missing[0]
		return nil, nil, &InputError{Key: "inputs." + in.Key, Message: in.Label + " is required"}
	}
	run, err := s.records.CreateRun(ctx, &Run{
		InstallID: up.install.ID, Kind: RunUpdate, FromRelease: up.install.Release, ToRelease: up.release.Tag,
		Status: RunRunning, Step: "Starting", CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, nil, err
	}
	started := copyRun(run)
	s.start(func() { s.runUpdate(caller, up.install, run, up) })
	return started, up.generated, nil
}

func (s *Service) planUpdate(ctx context.Context, caller *user.User, id int64, given map[string]string) (*updatePlan, error) {
	if err := user.Allow(caller, user.ResTemplates, RoleToInstall, ""); err != nil {
		return nil, err
	}
	in, err := s.records.Install(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Status != StatusInstalled {
		return nil, ErrNotInstalled
	}
	if in.Manifest == nil {
		return nil, errors.New("this installation did not record its template, so there is nothing to compare the catalog's with")
	}
	if busy, err := s.busy(ctx, id); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrBusy
	}
	chosen, m, err := s.fetch(ctx, in.Owner, in.Repo, "")
	if err != nil {
		return nil, err
	}

	up := &updatePlan{
		plan: &plan{
			owner: in.Owner, repo: in.Repo, release: chosen, manifest: m,
			installationName: in.InstallationName,
			project:          in.Project, environment: in.Environment,
			databases: map[string]string{}, stores: map[string]string{}, apps: map[string]string{},
			storeInputs: map[string]string{}, inputs: map[string]string{}, generated: map[string]string{},
		},
		install: in, old: in.Manifest,
		newDatabases: map[string]bool{}, newStores: map[string]bool{}, newApps: map[string]bool{},
		newBuckets: map[string][]string{}, changed: map[string]*appChange{},
	}
	if up.available = !sameJSON(unpinned(in.Manifest), unpinned(m)); !up.available {
		return up, nil
	}

	if err := s.compareDatabases(ctx, caller, up); err != nil {
		return nil, err
	}
	if err := s.compareStores(ctx, caller, up); err != nil {
		return nil, err
	}
	if err := s.compareApps(ctx, caller, up); err != nil {
		return nil, err
	}
	if err := s.answerUpdate(ctx, caller, up, given); err != nil {
		return nil, err
	}
	up.describeWiring()
	return up, nil
}

func (s *Service) compareDatabases(ctx context.Context, caller *user.User, up *updatePlan) error {
	for _, db := range up.manifest.Databases {
		if name, ok := up.install.resource(KindDatabase, db.Key); ok {
			up.databases[db.Key] = name
			for _, old := range up.old.Databases {
				if old.Key != db.Key {
					continue
				}
				if old.Engine != db.Engine || deref(old.Version) != deref(db.Version) {
					up.change(ActionKeep, KindDatabase, name, fmt.Sprintf(
						"the release asks for %s %s, and a database's engine and version are fixed once it exists",
						db.Engine, or(deref(db.Version), "latest")))
				}
				// Extensions are **not** applied by an update, even
				// though a database can gain one: installing replaces
				// the container, and a template update is not where
				// somebody expects their database to go away for a few
				// seconds. It is said here instead, with where to do it.
				if !slices.Equal(old.Extensions, db.Extensions) {
					up.change(ActionKeep, KindDatabase, name, fmt.Sprintf(
						"the release asks for %s; an update does not touch an existing database, so install them on the database itself",
						or(strings.Join(db.Extensions, ", "), "no extensions")))
				}
			}
			continue
		}
		key := "databases." + db.Key
		name := up.componentName(db.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if _, err := s.datastores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "database", Name: name}
		} else if !errors.Is(err, datastore.ErrNotFound) {
			return err
		}
		up.databases[db.Key] = name
		up.newDatabases[db.Key] = true
		detail := strings.TrimSpace(db.Engine + " " + deref(db.Version))
		if len(db.Extensions) > 0 {
			// On the line that says what will be created, because this
			// is the only moment it can be decided: extensions are fixed
			// once the database exists.
			detail += " with " + strings.Join(db.Extensions, ", ")
		}
		up.change(ActionCreate, KindDatabase, name, detail)
	}
	up.keepRemoved(KindDatabase, func(key string) bool {
		return slices.ContainsFunc(up.manifest.Databases, func(d template.NormalizedDatabase) bool { return d.Key == key })
	})
	return nil
}

func (s *Service) compareStores(ctx context.Context, caller *user.User, up *updatePlan) error {
	for _, st := range up.manifest.Stores {
		if name, ok := up.install.resource(KindStore, st.Key); ok {
			up.stores[st.Key] = name
			for _, old := range up.old.Stores {
				if old.Key != st.Key {
					continue
				}
				for _, bucket := range st.Buckets {
					if !slices.Contains(old.Buckets, bucket) {
						up.newBuckets[st.Key] = append(up.newBuckets[st.Key], bucket)
						up.change(ActionCreate, "bucket", bucket, "in "+name)
					}
				}
			}
			continue
		}
		key := "stores." + st.Key
		name := up.componentName(st.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if _, err := s.stores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "object store", Name: name}
		} else if !errors.Is(err, objectstore.ErrNotFound) {
			return err
		}
		up.stores[st.Key] = name
		up.newStores[st.Key] = true
		up.change(ActionCreate, KindStore, name, "")
	}
	up.keepRemoved(KindStore, func(key string) bool {
		return slices.ContainsFunc(up.manifest.Stores, func(s template.NormalizedStore) bool { return s.Key == key })
	})
	return nil
}
