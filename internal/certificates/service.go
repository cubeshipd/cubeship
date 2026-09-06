package certificates

import (
	"bufio"
	"context"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// logTail is how much of Traefik's log is read looking for a reason.
//
// Far enough back to cover a certificate that failed hours ago — ACME
// retries are quiet and the failure that matters is often the first one
// — and short enough that reading it is not a job.
const logTail = "5000"

// maxComplaints bounds what a report carries out of the log. Enough to
// see a pattern, not so much that the page becomes the log.
const maxComplaints = 5

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
	report.TraefikSays = s.explain(ctx, report.Missing)
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
		// Both are routed by Traefik with the same resolver, and neither
		// belongs to an app — but they are routed by different means.
		// The daemon's own name comes from the file the daemon writes,
		// so it is there whenever there is a domain. The registry's
		// comes from its container's labels, and a container keeps the
		// labels it was created with: one made before the domain existed
		// carries no router at all, and Traefik has never heard of the
		// name.
		registryHost := settings.RegistryHostFor(domain)
		out = append(out,
			ServedHost{Host: settings.APIHostFor(domain), Instance: true, Deployed: true},
			ServedHost{
				Host: registryHost, Instance: true,
				Deployed: s.registryRouted(ctx, registryHost),
			})
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

// registryRouted reports whether the registry's container is running
// with a Traefik router for this name.
//
// Without an Engine there is no way to tell, and the answer is yes: a
// report that cannot look must not accuse.
func (s *Service) registryRouted(ctx context.Context, host string) bool {
	if s.engine == nil {
		return true
	}
	info, err := s.engine.InspectContainerByName(ctx, bootstrap.RegistryContainerName)
	if err != nil || !info.Running {
		return false
	}
	for key, value := range info.Labels {
		if strings.HasPrefix(key, "traefik.http.routers.") && strings.HasSuffix(key, ".rule") &&
			strings.Contains(strings.ToLower(value), app.NormalizeHost(host)) {
			return true
		}
	}
	return false
}

// explain fills in what Traefik said about the names that have no
// certificate, and returns what it has been complaining about lately.
//
// It reads the container's log, because Traefik has no API for this and
// the ACME error appears nowhere else. That makes it a quotation rather
// than a contract: a log line that stops matching leaves the field
// empty, and the report is still the report.
//
// The unattributed lines matter as much as the attributed ones. The
// commonest failure on a default install is a rate limit — sslip.io is
// not on the Public Suffix List, so Let's Encrypt counts every name
// under it against one weekly allowance shared with everybody else
// using the service — and that refusal does not always name the host it
// was asked for.
func (s *Service) explain(ctx context.Context, missing []Missing) []string {
	if s.engine == nil || len(missing) == 0 {
		return nil
	}
	pending := false
	for _, m := range missing {
		if m.Reason == ReasonPending {
			pending = true
			break
		}
	}
	if !pending {
		return nil
	}

	info, err := s.engine.InspectContainerByName(ctx, bootstrap.TraefikContainerName)
	if err != nil || info.ID == "" {
		return nil
	}
	logs, err := s.engine.Logs(ctx, info.ID, logTail)
	if err != nil {
		return nil
	}
	defer logs.Close()

	lines := complaints(logs)
	for i := range missing {
		if missing[i].Reason != ReasonPending {
			continue
		}
		missing[i].Detail = about(lines, missing[i].Host)
	}
	if len(lines) > maxComplaints {
		lines = lines[len(lines)-maxComplaints:]
	}
	return lines
}

// complaints keeps the lines where Traefik said something went wrong
// with a certificate — or with reading the Engine, which comes to the
// same thing — oldest first.
//
// **A failing provider belongs here.** Traefik's Docker provider asks
// for a fixed API version, and an Engine that will not answer it leaves
// Traefik seeing no container at all: every router that comes from a
// label silently does not exist, so every name behind one is served the
// default self-signed certificate and reported as waiting. Nothing in
// the ACME wording says that. The provider's own line does, and it is
// the only place it is written down.
//
// Docker frames the log, and a frame header is a few bytes of binary in
// front of each line. Nothing here needs to be exact about it: the
// scanner reads lines, the header lands at the start of one, and the
// unprintable bytes are dropped rather than parsed.
func complaints(r io.Reader) []string {
	var out []string
	seen := map[string]bool{}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Map(printable, scanner.Text()))
		lower := strings.ToLower(line)
		about := strings.Contains(lower, "acme") || strings.Contains(lower, "certificate") ||
			strings.Contains(lower, "provider")
		if !about {
			continue
		}
		if !strings.Contains(lower, "error") && !strings.Contains(lower, "unable") &&
			!strings.Contains(lower, "too many") && !strings.Contains(lower, "fail") {
			continue
		}
		// Traefik retries, so the same refusal appears over and over —
		// with a new timestamp each time, and the provider's with a new
		// backoff too. The page wants the distinct things that are
		// wrong, not how often it tried, so every number is out of the
		// key.
		key := digits.ReplaceAllString(timestamps.ReplaceAllString(lower, ""), "#")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
}

// timestamps is what makes one retry look like another: the same
// refusal, logged again a minute later.
var timestamps = regexp.MustCompile(`(?i)"?time"?[=:]\s*"?[^"\s]+"?`)

// digits finishes the job for a line that carries its own backoff —
// "retrying in 6.055130443s" is the same complaint as "retrying in
// 1.079379231s", and without this every retry is its own entry.
var digits = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)

// about is the last distinct thing said about one name.
func about(lines []string, host string) string {
	host = app.NormalizeHost(host)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(lines[i]), host) {
			return lines[i]
		}
	}
	return ""
}

// printable drops the frame header's control bytes, which would
// otherwise land in a JSON string somebody reads.
func printable(r rune) rune {
	if r < 32 || r == 127 {
		return -1
	}
	return r
}
