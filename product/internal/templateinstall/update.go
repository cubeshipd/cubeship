package templateinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/objectstore"
	"cubeship/internal/user"
	"cubeship/template"
)

// What a change in a preview does.
const (
	ActionCreate = "create"
	ActionChange = "change"
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

// Preview is what updating an installation to a release would do.
type Preview struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Changes []Change `json:"changes"`
	// Inputs are questions the release asks that the installation has no
	// answer for, including a secret it can no longer read back.
	Inputs []template.NormalizedInput `json:"inputs"`
}

// UpdateRequest is what an update is asked to do.
type UpdateRequest struct {
	// Release is a tag the catalog accepted. Empty is the newest one.
	Release string
	// Inputs answers the questions the preview listed.
	Inputs map[string]string
}

type appChange struct {
	source   bool
	settings bool
	env      []string
	domains  []template.NormalizedDomain
	attach   []template.NormalizedAttach
	volumes  []template.NormalizedVolume
}

func (c *appChange) empty() bool {
	return !c.source && !c.settings && len(c.env) == 0 && len(c.domains) == 0 && len(c.attach) == 0 &&
		len(c.volumes) == 0
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

// PreviewUpdate says what updating an installation to a release would do,
// and creates and changes nothing.
func (s *Service) PreviewUpdate(ctx context.Context, caller *user.User, id int64, release string) (*Preview, error) {
	up, err := s.planUpdate(ctx, caller, id, release, nil)
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
	return &Preview{From: up.install.Release, To: up.release.Tag, Changes: changes, Inputs: inputs}, nil
}

// Update applies a release to an installation in the background. Secrets
// the release adds, and the instance generated, come back here once.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, req UpdateRequest) (*Run, map[string]string, error) {
	up, err := s.planUpdate(ctx, caller, id, req.Release, req.Inputs)
	if err != nil {
		return nil, nil, err
	}
	if up.release.Tag == up.install.Release {
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

// Uninstall deletes an installation's apps in the background — and its
// databases and stores too, unless keepData.
func (s *Service) Uninstall(ctx context.Context, caller *user.User, id int64, keepData bool) (*Run, error) {
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
	if busy, err := s.busy(ctx, id); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrBusy
	}
	run, err := s.records.CreateRun(ctx, &Run{
		InstallID: id, Kind: RunUninstall, FromRelease: in.Release,
		Status: RunRunning, Step: "Starting", KeepData: keepData, CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, err
	}
	started := copyRun(run)
	s.start(func() { s.runUninstall(caller, in, run) })
	return started, nil
}

func (s *Service) planUpdate(ctx context.Context, caller *user.User, id int64, release string, given map[string]string) (*updatePlan, error) {
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
		return nil, errors.New("this installation did not record its template, so there is nothing to compare a release with")
	}
	if busy, err := s.busy(ctx, id); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrBusy
	}
	chosen, m, err := s.fetch(ctx, in.Owner, in.Repo, release)
	if err != nil {
		return nil, err
	}

	up := &updatePlan{
		plan: &plan{
			owner: in.Owner, repo: in.Repo, release: chosen, manifest: m,
			project: in.Project, environment: in.Environment,
			databases: map[string]string{}, stores: map[string]string{}, apps: map[string]string{},
			storeInputs: map[string]string{}, inputs: map[string]string{}, generated: map[string]string{},
		},
		install: in, old: in.Manifest,
		newDatabases: map[string]bool{}, newStores: map[string]bool{}, newApps: map[string]bool{},
		newBuckets: map[string][]string{}, changed: map[string]*appChange{},
	}
	if chosen.Tag == in.Release {
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
				if old.Key == db.Key && (old.Engine != db.Engine || deref(old.Version) != deref(db.Version)) {
					up.change(ActionKeep, KindDatabase, name, fmt.Sprintf(
						"the release asks for %s %s, and a database's engine and version are fixed once it exists",
						db.Engine, or(deref(db.Version), "latest")))
				}
			}
			continue
		}
		key := "databases." + db.Key
		if err := checkSlug(key, db.Name); err != nil {
			return err
		}
		if _, err := s.datastores.Resolve(ctx, caller, db.Name, RoleToInstall); err == nil {
			return &TakenError{Kind: "database", Name: db.Name}
		} else if !errors.Is(err, datastore.ErrNotFound) {
			return err
		}
		up.databases[db.Key] = db.Name
		up.newDatabases[db.Key] = true
		up.change(ActionCreate, KindDatabase, db.Name, strings.TrimSpace(db.Engine+" "+deref(db.Version)))
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
		if err := checkSlug(key, st.Name); err != nil {
			return err
		}
		if _, err := s.stores.Resolve(ctx, caller, st.Name, RoleToInstall); err == nil {
			return &TakenError{Kind: "object store", Name: st.Name}
		} else if !errors.Is(err, objectstore.ErrNotFound) {
			return err
		}
		up.stores[st.Key] = st.Name
		up.newStores[st.Key] = true
		up.change(ActionCreate, KindStore, st.Name, "")
	}
	up.keepRemoved(KindStore, func(key string) bool {
		return slices.ContainsFunc(up.manifest.Stores, func(s template.NormalizedStore) bool { return s.Key == key })
	})
	return nil
}

func (s *Service) compareApps(ctx context.Context, caller *user.User, up *updatePlan) error {
	for _, a := range up.manifest.Apps {
		if name, ok := up.install.resource(KindApp, a.Key); ok {
			ref, err := app.ParseReference(name)
			if err != nil {
				return err
			}
			up.apps[a.Key] = ref.Name
			var old template.NormalizedApp
			for _, o := range up.old.Apps {
				if o.Key == a.Key {
					old = o
				}
			}
			ch := compareApp(old, a)
			if ch.empty() {
				continue
			}
			up.changed[a.Key] = ch
			if ch.source {
				up.change(ActionChange, KindApp, ref.String(), sourceLabel(old.Source)+" → "+sourceLabel(a.Source))
			}
			if ch.settings {
				up.change(ActionChange, KindApp, ref.String(), "health check, limits or scale")
			}
			for _, env := range ch.env {
				detail := "set on " + ref.String()
				if _, had := old.Env[env]; had {
					detail = "changed on " + ref.String() + "; a value you set by hand is replaced"
				}
				up.change(ActionChange, "variable", env, detail)
			}
			for env := range old.Env {
				if _, still := a.Env[env]; !still {
					up.change(ActionKeep, "variable", env, "no longer in the template; left on "+ref.String())
				}
			}
			continue
		}
		key := "apps." + a.Key
		if err := checkSlug(key, a.Name); err != nil {
			return err
		}
		ref := app.Reference{Project: up.project, Environment: up.environment, Name: a.Name}
		if _, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
			return &TakenError{Kind: "app", Name: ref.String()}
		} else if !errors.Is(err, app.ErrNotFound) {
			return err
		}
		up.apps[a.Key] = a.Name
		up.newApps[a.Key] = true
		up.change(ActionCreate, KindApp, ref.String(), sourceLabel(a.Source))
	}
	up.keepRemoved(KindApp, func(key string) bool {
		return slices.ContainsFunc(up.manifest.Apps, func(a template.NormalizedApp) bool { return a.Key == key })
	})
	return nil
}

// keepRemoved lists what the installation has of a kind that the release
// no longer declares. It stays: an update never deletes.
func (up *updatePlan) keepRemoved(kind string, declared func(key string) bool) {
	for _, r := range up.install.Resources {
		if r.Kind == kind && !declared(r.Key) {
			up.change(ActionKeep, kind, r.Name, "no longer in the template; left as it is")
		}
	}
}

func compareApp(old, a template.NormalizedApp) *appChange {
	ch := &appChange{
		source:   !sameJSON(old.Source, a.Source),
		settings: !sameJSON(settingsOf(old), settingsOf(a)),
	}
	for _, name := range envNames(a) {
		if value, ok := old.Env[name]; !ok || value != a.Env[name] {
			ch.env = append(ch.env, name)
		}
	}
	for _, d := range a.Domains {
		if !slices.ContainsFunc(old.Domains, func(o template.NormalizedDomain) bool { return o.Host == d.Host }) {
			ch.domains = append(ch.domains, d)
		}
	}
	for _, at := range a.Attach {
		if !slices.ContainsFunc(old.Attach, func(o template.NormalizedAttach) bool {
			return o.Kind == at.Kind && o.Key == at.Key && o.Prefix == at.Prefix && deref(o.Bucket) == deref(at.Bucket)
		}) {
			ch.attach = append(ch.attach, at)
		}
	}
	// Added, never removed: a volume a release no longer declares keeps its
	// data, as every other resource an update drops stays.
	for _, v := range a.Volumes {
		if !slices.ContainsFunc(old.Volumes, func(o template.NormalizedVolume) bool { return o.Path == v.Path }) {
			ch.volumes = append(ch.volumes, v)
		}
	}
	return ch
}

func settingsOf(a template.NormalizedApp) any {
	return struct {
		Health    *string
		Limits    *template.NormalizedLimits
		Scale     *int
		Spread    bool
		Autoscale *template.NormalizedAutoscale
	}{a.Health, a.Limits, a.Scale, a.Spread, a.Autoscale}
}

func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

func sourceLabel(src template.NormalizedSource) string {
	if src.Type == "image" {
		return src.Image + ":" + or(deref(src.Tag), "latest")
	}
	if ref := deref(src.Ref); ref != "" {
		return src.Repo + "@" + ref
	}
	return src.Repo
}

// answerUpdate settles the release's inputs: the installation's answers
// for the questions it already asked, a secret read back from the app
// that holds it, and the caller's answers — or, for a preview, the list
// of questions still to ask.
func (s *Service) answerUpdate(ctx context.Context, caller *user.User, up *updatePlan, given map[string]string) error {
	hosts := map[string]bool{}
	previously := map[string]template.NormalizedInput{}
	for _, in := range up.old.Inputs {
		previously[in.Key] = in
	}
	for _, in := range up.manifest.Inputs {
		if old, asked := previously[in.Key]; asked && old.Type == in.Type {
			if in.Type == "secret" {
				if value := given[in.Key]; value != "" {
					up.inputs[in.Key] = value
					continue
				}
				if value, ok := s.readSecret(ctx, caller, up, in.Key); ok {
					up.inputs[in.Key] = value
					continue
				}
				if up.needs(in.Key) {
					ask := in
					ask.Generate = nil
					up.missing = append(up.missing, ask)
				}
				continue
			}
			if value, ok := up.install.Answers[in.Key]; ok {
				up.inputs[in.Key] = value
				if in.Type == "store" {
					up.storeInputs[in.Key] = value
				}
				continue
			}
		}
		value := given[in.Key]
		if strings.TrimSpace(value) == "" && in.Default == nil && in.Required &&
			!(in.Type == "secret" && in.Generate != nil) {
			up.missing = append(up.missing, in)
			continue
		}
		if err := s.answerInput(ctx, caller, up.plan, in, value, hosts); err != nil {
			return err
		}
	}
	return nil
}

// readSecret finds a secret's value in the variables of an app the
// template gave it to whole — `APP_SECRET: ${input.appSecret}` — which is
// the only place a secret is kept.
func (s *Service) readSecret(ctx context.Context, caller *user.User, up *updatePlan, key string) (string, bool) {
	whole := "${input." + key + "}"
	for _, a := range up.old.Apps {
		for name, value := range a.Env {
			if value != whole {
				continue
			}
			refText, ok := up.install.resource(KindApp, a.Key)
			if !ok {
				continue
			}
			ref, err := app.ParseReference(refText)
			if err != nil {
				continue
			}
			own, _, err := s.apps.Env(ctx, caller, ref)
			if err != nil {
				continue
			}
			if found, ok := own[name]; ok && found != "" {
				return found, true
			}
		}
	}
	return "", false
}

// needs reports whether anything this update writes refers to an input.
func (up *updatePlan) needs(key string) bool {
	reference := "${input." + key + "}"
	for _, a := range up.manifest.Apps {
		var values []string
		switch ch := up.changed[a.Key]; {
		case up.newApps[a.Key]:
			for _, v := range a.Env {
				values = append(values, v)
			}
			for _, d := range a.Domains {
				values = append(values, d.Host)
			}
		case ch != nil:
			for _, name := range ch.env {
				values = append(values, a.Env[name])
			}
			for _, d := range ch.domains {
				values = append(values, d.Host)
			}
		}
		for _, v := range values {
			if strings.Contains(v, reference) {
				return true
			}
		}
	}
	return false
}

// describeWiring adds the domains and attachments an update gives apps
// that already exist, now that the answers they are made of are settled.
func (up *updatePlan) describeWiring() {
	for _, a := range up.manifest.Apps {
		ch := up.changed[a.Key]
		if ch == nil {
			continue
		}
		ref := up.ref(a.Key).String()
		for _, d := range ch.domains {
			host, _ := template.ReplaceReferences(d.Host, func(kind, key, _ string) (string, error) {
				if kind == "input" {
					return up.inputs[key], nil
				}
				return "", nil
			})
			up.change(ActionCreate, KindDomain, host, "on "+ref)
		}
		for _, at := range ch.attach {
			name := up.databases[at.Key]
			if at.Kind == "store" {
				name = up.storeName(at.Key)
			}
			up.change(ActionCreate, KindAttachment, name, "to "+ref)
		}
		for _, v := range ch.volumes {
			up.change(ActionCreate, KindVolume, v.Path, "on "+ref+"; it runs as one copy from here on")
		}
	}
}
