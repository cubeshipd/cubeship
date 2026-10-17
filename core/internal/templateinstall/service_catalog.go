package templateinstall

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/slug"
	"cubeship/internal/template"
	"cubeship/internal/user"
)

// Manifest is what one release creates and asks, read from its repository
// the way an install reads it — so choosing an older version shows the form
// that version installs with.
func (s *Service) Manifest(ctx context.Context, caller *user.User, owner, repo, release string) (*ReleaseManifest, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	chosen, m, err := s.readRelease(ctx, owner, repo, release)
	if err != nil {
		return nil, err
	}
	out := &ReleaseManifest{
		Release:  ReleaseOption{Tag: chosen.Tag, Commit: chosen.Commit, PublishedAt: chosen.PublishedAt},
		Manifest: m, Fits: true,
	}
	if err := s.fitsInstance(m); err != nil {
		if !errors.Is(err, ErrTooNew) {
			return nil, err
		}
		out.Fits, out.Problem = false, err.Error()
	}
	return out, nil
}

// fetch reads a release's file from its repository, validates it here, and
// refuses one this instance is too old for. An empty release is the newest
// the catalog accepted.
func (s *Service) fetch(ctx context.Context, owner, repo, release string) (CatalogRelease, *template.Normalized, error) {
	chosen, m, err := s.readRelease(ctx, owner, repo, release)
	if err != nil {
		return CatalogRelease{}, nil, err
	}
	if err := s.fitsInstance(m); err != nil {
		return CatalogRelease{}, nil, err
	}
	return chosen, m, nil
}

// fitsInstance refuses a manifest whose minCubeship this instance does not
// satisfy.
func (s *Service) fitsInstance(m *template.Normalized) error {
	if m.MinCubeship == nil {
		return nil
	}
	ok, err := template.Satisfies(*m.MinCubeship, s.version)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: it asks for %s and this instance runs %s", ErrTooNew, *m.MinCubeship, s.version)
	}
	return nil
}

// readRelease reads a release's file from its repository and validates it.
func (s *Service) readRelease(ctx context.Context, owner, repo, release string) (CatalogRelease, *template.Normalized, error) {
	c, err := s.source()
	if err != nil {
		return CatalogRelease{}, nil, err
	}
	releases, err := c.Releases(ctx, owner, repo)
	if err != nil {
		return CatalogRelease{}, nil, err
	}
	var chosen *CatalogRelease
	for i := range releases {
		if releases[i].Status == "accepted" && (release == "" || releases[i].Tag == release) {
			chosen = &releases[i]
			break
		}
	}
	if chosen == nil {
		return CatalogRelease{}, nil, ErrReleaseNotFound
	}
	source, err := c.Source(ctx, owner, repo, chosen.Commit)
	if err != nil {
		return CatalogRelease{}, nil, err
	}
	result := template.Validate(source)
	if !result.OK {
		var refused []template.Diagnostic
		for _, d := range result.Diagnostics {
			if d.Severity == template.Error {
				refused = append(refused, d)
			}
		}
		return CatalogRelease{}, nil, &InvalidTemplateError{Diagnostics: refused}
	}
	pinMonorepoSources(result.Manifest, owner, repo, chosen.Commit)
	return *chosen, result.Manifest, nil
}

// BuildKit uses "#commit:directory" to build only this template's context.
// Pin the cataloged commit so later edits to main cannot change an install.
func pinMonorepoSources(m *template.Normalized, owner, repo, commit string) {
	if owner != "cubeshipd" {
		return
	}
	for i := range m.Apps {
		src := &m.Apps[i].Source
		if src.Repo == "https://github.com/cubeshipd/cubeship" && src.Ref != nil && *src.Ref == "master:templates/"+repo {
			ref := commit + ":templates/" + repo
			src.Ref = &ref
		}
		if src.Repo == "https://github.com/cubeshipd/cubeship-templates" && src.Ref != nil && *src.Ref == "main:"+repo {
			ref := commit + ":" + repo
			src.Ref = &ref
		}
	}
}

func (s *Service) prepare(ctx context.Context, caller *user.User, req Request) (*plan, error) {
	release, m, err := s.fetch(ctx, req.Owner, req.Repo, req.Release)
	if err != nil {
		return nil, err
	}

	p := &plan{
		owner: req.Owner, repo: req.Repo, release: release, manifest: m,
		project:     or(req.Project, m.Project),
		environment: or(req.Environment, m.Environment),
		databases:   map[string]string{}, stores: map[string]string{}, apps: map[string]string{},
		storeInputs: map[string]string{}, inputs: map[string]string{}, generated: map[string]string{},
	}
	if err := checkSlug("project", p.project); err != nil {
		return nil, err
	}
	if err := checkSlug("environment", p.environment); err != nil {
		return nil, err
	}
	if _, err := s.projects.Resolve(ctx, caller, p.project, RoleToInstall); errors.Is(err, project.ErrNotFound) {
		p.createProject = true
		p.createEnvironment = p.environment != project.ProductionEnvSlug
	} else if err != nil {
		return nil, err
	} else if _, err := s.projects.ResolveEnvironment(ctx, caller, p.project, p.environment, RoleToInstall); errors.Is(err, project.ErrEnvironmentNotFound) {
		p.createEnvironment = true
	} else if err != nil {
		return nil, err
	}

	if err := s.nameDatabases(ctx, caller, p, req.Databases); err != nil {
		return nil, err
	}
	if err := s.nameStores(ctx, caller, p, req.Stores); err != nil {
		return nil, err
	}
	if err := s.nameApps(ctx, caller, p, req.Apps); err != nil {
		return nil, err
	}
	if err := s.answer(ctx, caller, p, req.Inputs); err != nil {
		return nil, err
	}
	if err := s.checkTCPPorts(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) nameDatabases(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, db := range p.manifest.Databases {
		key := "databases." + db.Key
		name := or(strings.TrimSpace(overrides[db.Key]), db.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two databases would both be called " + name}
		}
		used[name] = true
		if _, err := s.datastores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "database", Name: name}
		} else if !errors.Is(err, datastore.ErrNotFound) {
			return err
		}
		p.databases[db.Key] = name
	}
	return nil
}

func (s *Service) nameStores(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, st := range p.manifest.Stores {
		key := "stores." + st.Key
		name := or(strings.TrimSpace(overrides[st.Key]), st.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two object stores would both be called " + name}
		}
		used[name] = true
		if _, err := s.stores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "object store", Name: name}
		} else if !errors.Is(err, objectstore.ErrNotFound) {
			return err
		}
		p.stores[st.Key] = name
	}
	return nil
}

func (s *Service) nameApps(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, a := range p.manifest.Apps {
		key := "apps." + a.Key
		name := or(strings.TrimSpace(overrides[a.Key]), a.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two apps would both be called " + name}
		}
		used[name] = true
		// A project or environment about to be created has no apps.
		if !p.createProject && !p.createEnvironment {
			ref := app.Reference{Project: p.project, Environment: p.environment, Name: name}
			if _, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
				return &TakenError{Kind: "app", Name: ref.String()}
			} else if !errors.Is(err, app.ErrNotFound) {
				return err
			}
		}
		p.apps[a.Key] = name
	}
	return nil
}

// checkTCPPorts refuses, before anything is created, a host port the
// answers name that no instance publishes on or something here already
// holds. A port left for the instance to pick cannot be refused.
func (s *Service) checkTCPPorts(ctx context.Context, p *plan) error {
	seen := map[int]bool{}
	for _, a := range p.manifest.Apps {
		for _, tp := range a.TCP {
			key := "apps." + a.Key
			if tp.Host != nil {
				if input, ok := strings.CutPrefix(*tp.Host, "${input."); ok {
					key = "inputs." + strings.TrimSuffix(input, "}")
				}
			}
			host, err := tcpHostPort(p, tp)
			if err != nil {
				return &InputError{Key: key, Message: err.Error()}
			}
			if host == 0 {
				continue
			}
			if host < app.MinTCPHostPort || host > 65535 {
				return &InputError{Key: key, Message: fmt.Sprintf("a host port is from %d to 65535", app.MinTCPHostPort)}
			}
			if seen[host] {
				return &InputError{Key: key, Message: fmt.Sprintf("host port %d is the answer for another port as well", host)}
			}
			seen[host] = true
			taken, err := s.apps.TCPPortTaken(ctx, host)
			if err != nil {
				return err
			}
			if taken {
				return &TakenError{Kind: "host port", Name: strconv.Itoa(host)}
			}
		}
	}
	return nil
}

// answers is every answer that is not a secret: what an installation
// keeps for the next update.
func (p *plan) answers() map[string]string {
	out := map[string]string{}
	for _, in := range p.manifest.Inputs {
		if value, ok := p.inputs[in.Key]; ok && in.Type != "secret" {
			out[in.Key] = value
		}
	}
	return out
}

// answer settles every input: the caller's answer, else the template's
// default, else a generated secret — and checks each against its type.
func (s *Service) answer(ctx context.Context, caller *user.User, p *plan, given map[string]string) error {
	hosts := map[string]bool{}
	for _, in := range p.manifest.Inputs {
		if err := s.answerInput(ctx, caller, p, in, given[in.Key], hosts); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) answerInput(ctx context.Context, caller *user.User, p *plan, in template.NormalizedInput, value string, hosts map[string]bool) error {
	key := "inputs." + in.Key
	if in.Type != "secret" {
		value = strings.TrimSpace(value)
	}
	if value == "" && in.Default != nil {
		value = fmt.Sprint(in.Default)
	}
	if value == "" && in.Type == "secret" && in.Generate != nil {
		generated, err := generateSecret(*in.Generate)
		if err != nil {
			return err
		}
		value = generated
		p.generated[in.Key] = generated
	}
	if value == "" {
		if in.Required {
			return &InputError{Key: key, Message: in.Label + " is required"}
		}
		return nil
	}

	switch in.Type {
	case "domain":
		host := app.NormalizeHost(value)
		if !app.ValidHost(host) {
			return &InputError{Key: key, Message: value + " is not a hostname"}
		}
		if hosts[host] {
			return &InputError{Key: key, Message: host + " is the answer to another domain as well"}
		}
		hosts[host] = true
		taken, err := s.apps.HostTaken(ctx, host)
		if err != nil {
			return err
		}
		if taken {
			return &TakenError{Kind: "domain", Name: host}
		}
		value = host
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return &InputError{Key: key, Message: value + " is not a number"}
		}
		if in.Min != nil && n < *in.Min {
			return &InputError{Key: key, Message: fmt.Sprintf("is at least %v", *in.Min)}
		}
		if in.Max != nil && n > *in.Max {
			return &InputError{Key: key, Message: fmt.Sprintf("is at most %v", *in.Max)}
		}
	case "choice":
		if !slices.Contains(in.Options, value) {
			return &InputError{Key: key, Message: "is one of " + strings.Join(in.Options, ", ")}
		}
	case "text":
		if in.Pattern != "" {
			// A template's pattern matches the whole answer, the way an
			// HTML pattern attribute does.
			re, err := regexp.Compile("^(?:" + in.Pattern + ")$")
			if err != nil {
				return &InputError{Key: key, Message: "the template's pattern cannot be read here: " + err.Error()}
			}
			if !re.MatchString(value) {
				return &InputError{Key: key, Message: "does not match " + in.Pattern}
			}
		}
	case "store":
		if _, err := s.stores.Resolve(ctx, caller, value, RoleToInstall); errors.Is(err, objectstore.ErrNotFound) {
			return &InputError{Key: key, Message: "there is no object store called " + value}
		} else if err != nil {
			return err
		}
		p.storeInputs[in.Key] = value
	}
	p.inputs[in.Key] = value
	return nil
}

func checkSlug(key, value string) error {
	if slug.Reserved(value) {
		return &InputError{Key: key, Message: slug.ErrReserved.Error()}
	}
	if !slug.Valid(value) {
		return &InputError{Key: key, Message: value + " " + slug.ErrInvalid.Error()}
	}
	return nil
}

const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generateSecret is n letters and digits: long enough is the whole of its
// strength, and nothing that reads it has to quote a symbol.
func generateSecret(n int) (string, error) {
	out := make([]byte, n)
	limit := big.NewInt(int64(len(secretAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		out[i] = secretAlphabet[v.Int64()]
	}
	return string(out), nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
