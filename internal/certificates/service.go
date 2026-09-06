package certificates

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// TraefikContainer is the container whose log says why a certificate was
// not issued. It must match bootstrap.TraefikContainerOpts.
const TraefikContainer = "cubeship-traefik"

// logTail is how much of Traefik's log is read looking for a reason. Far
// enough back to cover the last few deploys, short enough that reading
// it is not a job.
const logTail = "2000"

// Engine is the little of Docker this module needs: the ACME failures
// are in Traefik's log and nowhere else.
//
// It is optional. Without it the report is the same minus the quotations
// — which is what a test gets, and what an instance whose Docker client
// cannot inspect containers gets.
type Engine interface {
	InspectContainerByName(ctx context.Context, name string) (dockerx.ContainerInfo, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
}

// Service reads the certificate store and lines it up against what this
// instance routes.
//
// It sits above app for the same reason registry does: the names that
// should have certificates are the apps' names, and only app knows them.
type Service struct {
	settings *settings.Service
	apps     *app.Service
	dataDir  string

	engine Engine
}

func NewService(cfg *settings.Service, apps *app.Service, dataDir string) *Service {
	return &Service{settings: cfg, apps: apps, dataDir: dataDir}
}

// SetEngine wires the Docker client the log quotations come from. The
// server does it when its client can inspect containers.
func (s *Service) SetEngine(e Engine) { s.engine = e }

// Report is the whole answer: every certificate this instance holds, and
// every name it routes that has none.
//
// An admin's, like everything else about how the instance is wired. A
// member sees an app's domains on the app; whether the instance managed
// to get a certificate for one is the operator's business.
func (s *Service) Report(ctx context.Context, caller *user.User) (Report, error) {
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return Report{}, err
	}

	values, err := s.settings.Load(ctx)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		TLSEnabled:   values.HasTLS(),
		Certificates: []Certificate{},
		Missing:      []Missing{},
	}

	served, err := s.servedHosts(ctx, values)
	if err != nil {
		return Report{}, err
	}

	email, certs, err := readStore(StorePath(s.dataDir))
	if err != nil && !errors.Is(err, ErrNoStore) {
		return Report{}, err
	}
	report.ACMEEmail = email

	report.Certificates, report.Missing = reconcile(certs, served, report.TLSEnabled)
	s.explain(ctx, report.Missing)
	return report, nil
}

// reconcile is the whole of the comparison, kept apart from where the
// two sides come from so it can be read — and tested — on its own.
//
// A certificate whose name nothing serves is an orphan: it stays valid
// and stays in the file, and the only thing wrong with it is that it was
// paid for. A served name with no certificate is missing one, and why
// depends on how far the name got.
func reconcile(certs []Certificate, served []ServedHost, tls bool) ([]Certificate, []Missing) {
	byHost := make(map[string]ServedHost, len(served))
	for _, h := range served {
		byHost[app.NormalizeHost(h.Host)] = h
	}

	out := make([]Certificate, 0, len(certs))
	covered := make(map[string]bool, len(certs))
	for _, c := range certs {
		covered[c.Host] = true
		for _, san := range c.SANs {
			covered[san] = true
		}
		if h, ok := byHost[c.Host]; ok {
			c.App, c.Instance = h.App, h.Instance
		} else {
			c.Orphan = true
		}
		out = append(out, c)
	}

	missing := make([]Missing, 0)
	for _, h := range served {
		host := app.NormalizeHost(h.Host)
		if covered[host] {
			continue
		}
		missing = append(missing, Missing{
			Host: host, App: h.App, Instance: h.Instance,
			Reason: reasonFor(h, tls),
		})
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Host < missing[j].Host })
	return out, missing
}

func reasonFor(h ServedHost, tls bool) Reason {
	switch {
	case !tls:
		return ReasonNoTLS
	case !h.Deployed:
		return ReasonNotDeployed
	default:
		return ReasonPending
	}
}

// servedHosts is every name this instance routes: its own two, and every
// name on every app.
//
// "Deployed" is the difference between a name Traefik knows about and
// one that only exists here. A container keeps the labels it was created
// with, so a name added after the last deploy is a name Traefik has
// never been told about — which is the commonest reason a certificate is
// missing, and the one whose answer is "redeploy".
func (s *Service) servedHosts(ctx context.Context, values settings.Values) ([]ServedHost, error) {
	var out []ServedHost

	if domain := values.Get(settings.Domain); domain != "" {
		// Both are routed by Traefik with the same resolver: the
		// dashboard and API at the domain itself, the registry beside
		// it. Neither belongs to an app.
		out = append(out,
			ServedHost{Host: settings.APIHostFor(domain), Instance: true, Deployed: true},
			ServedHost{Host: settings.RegistryHostFor(domain), Instance: true, Deployed: true})
	}

	apps, err := s.apps.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	domains, err := s.apps.Repo().DomainsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		reference := app.ReferenceOf(a).String()
		for _, d := range domains[a.ID] {
			out = append(out, ServedHost{
				Host: d.Host,
				App:  reference,
				// The container carries the labels, so a name is only
				// really routed once something is running with it.
				Deployed: a.ContainerID != "",
			})
		}
	}
	return out, nil
}

// explain fills in what Traefik said about each name it could not get a
// certificate for.
//
// It reads the container's log, because Traefik has no API for this and
// the ACME error appears nowhere else. That makes it a quotation rather
// than a contract: a log line that stops matching leaves the field
// empty, and the report is still the report.
func (s *Service) explain(ctx context.Context, missing []Missing) {
	if s.engine == nil || len(missing) == 0 {
		return
	}
	pending := false
	for _, m := range missing {
		if m.Reason == ReasonPending {
			pending = true
			break
		}
	}
	if !pending {
		return
	}

	info, err := s.engine.InspectContainerByName(ctx, TraefikContainer)
	if err != nil || info.ID == "" {
		return
	}
	logs, err := s.engine.Logs(ctx, info.ID, logTail)
	if err != nil {
		return
	}
	defer logs.Close()

	lines := errorLines(logs)
	for i := range missing {
		if missing[i].Reason != ReasonPending {
			continue
		}
		if line, ok := lines[app.NormalizeHost(missing[i].Host)]; ok {
			missing[i].Detail = line
		}
	}
}

// errorLines keeps the last thing Traefik complained about, per host.
//
// Docker frames the log, and a frame header is a few bytes of binary in
// front of each line. Nothing here needs to be exact about it: the
// scanner reads lines, the header lands at the start of one, and the
// host is looked for anywhere in it.
func errorLines(r io.Reader) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Map(printable, scanner.Text()))
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "error") && !strings.Contains(lower, "unable") {
			continue
		}
		if !strings.Contains(lower, "acme") && !strings.Contains(lower, "certificate") {
			continue
		}
		// The host appears in the message; which one it is decides
		// whose line this is.
		for _, field := range strings.FieldsFunc(lower, func(r rune) bool {
			return r == '"' || r == ' ' || r == '\'' || r == ',' || r == ':'
		}) {
			if strings.Contains(field, ".") {
				out[app.NormalizeHost(field)] = line
			}
		}
	}
	return out
}

// printable drops the frame header's control bytes, which would
// otherwise land in a JSON string somebody reads.
func printable(r rune) rune {
	if r < 32 || r == 127 {
		return -1
	}
	return r
}
