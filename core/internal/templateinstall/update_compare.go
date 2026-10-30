package templateinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/template"
	"cubeship/internal/user"
)

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
		name := up.componentName(a.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		ref := app.Reference{Project: up.project, Environment: up.environment, Name: name}
		if _, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
			return &TakenError{Kind: "app", Name: ref.String()}
		} else if !errors.Is(err, app.ErrNotFound) {
			return err
		}
		up.apps[a.Key] = name
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
		source:     !sameJSON(unpinnedSource(old.Source), unpinnedSource(a.Source)),
		settings:   !sameJSON(settingsOf(old), settingsOf(a)),
		networking: !sameJSON(networkingOf(old), networkingOf(a)),
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
	for _, d := range old.Domains {
		if !slices.ContainsFunc(a.Domains, func(n template.NormalizedDomain) bool { return n.Host == d.Host }) {
			ch.dropped = append(ch.dropped, d)
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
	// Added by container port, and never removed, for the same reason.
	for _, tp := range a.TCP {
		if !slices.ContainsFunc(old.TCP, func(o template.NormalizedTCP) bool { return o.Port == tp.Port }) {
			ch.tcp = append(ch.tcp, tp)
		}
	}
	return ch
}

func networkingOf(a template.NormalizedApp) any {
	return struct {
		Ports  []int
		Checks []template.NormalizedHealthCheck
	}{a.InternalPorts, a.HealthChecks}
}

func settingsOf(a template.NormalizedApp) any {
	return struct {
		Health        *string
		Limits        *template.NormalizedLimits
		Scale         *int
		Spread        bool
		Autoscale     *template.NormalizedAutoscale
		InternalPorts []int
		HealthChecks  []template.NormalizedHealthCheck
	}{a.Health, a.Limits, a.Scale, a.Spread, a.Autoscale, a.InternalPorts, a.HealthChecks}
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

// hostOf is a domain's host with the answers it is made of. Only inputs
// can name a host.
func hostOf(d template.NormalizedDomain, answers map[string]string) string {
	host, _ := template.ReplaceReferences(d.Host, func(kind, key, _ string) (string, error) {
		if kind == "input" {
			return answers[key], nil
		}
		return "", nil
	})
	return host
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
		for _, d := range ch.dropped {
			up.change(ActionRemove, KindDomain, hostOf(d, up.install.Answers), "from "+ref+"; no longer in the template")
		}
		for _, d := range ch.domains {
			up.change(ActionCreate, KindDomain, hostOf(d, up.inputs), "on "+ref)
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
		for _, tp := range ch.tcp {
			up.change(ActionCreate, KindTCPPort, strconv.Itoa(tp.Port), "published from "+ref+"; it runs as one copy on the control plane from here on")
		}
	}
}
