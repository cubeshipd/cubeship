package user

import (
	"errors"
	"fmt"
	"slices"
)

// Resource is a kind of thing on the instance that access is granted to.
type Resource string

const (
	// ResProjects is projects and environments: their variables, their
	// picture, creating and deleting them.
	ResProjects Resource = "projects"
	// ResApps is the apps inside projects: deploys, variables, volumes,
	// logs. Its items are project slugs.
	ResApps Resource = "apps"
	// ResDomains is the names apps answer at. Its items are project slugs.
	ResDomains      Resource = "domains"
	ResDatabases    Resource = "databases"
	ResStorage      Resource = "storage"
	ResServers      Resource = "servers"
	ResTemplates    Resource = "templates"
	ResBackups      Resource = "backups"
	ResRegistry     Resource = "registry"
	ResRegistries   Resource = "registries"
	ResGit          Resource = "git"
	ResDNS          Resource = "dns"
	ResCredentials  Resource = "credentials"
	ResCertificates Resource = "certificates"
	ResFirewall     Resource = "firewall"
	ResSettings     Resource = "settings"
	ResAudit        Resource = "audit"
)

// ResourceInfo is a resource as a role editor lists it. Items says the
// grant can name which ones; ItemsAre says what an item is.
type ResourceInfo struct {
	Resource Resource `json:"resource"`
	Items    bool     `json:"items"`
	ItemsAre string   `json:"items_are,omitempty"`
	Secrets  bool     `json:"secrets"`
	// Shell says the grant can open a shell inside what it names.
	Shell bool `json:"shell"`
}

// Resources is every resource, in the order a screen lists them. Users
// and roles are not here: they stay an admin's, because whoever can
// grant access can grant themselves all of it.
var Resources = []ResourceInfo{
	{ResProjects, true, "project", true, false},
	{ResApps, true, "project", true, true},
	{ResDomains, true, "project", false, false},
	{ResDatabases, true, "database", true, false},
	{ResStorage, true, "object store", true, false},
	{ResServers, false, "", false, false},
	{ResTemplates, false, "", false, false},
	{ResBackups, false, "", true, false},
	{ResRegistry, false, "", false, false},
	{ResRegistries, false, "", false, false},
	{ResGit, false, "", false, false},
	{ResDNS, false, "", false, false},
	{ResCredentials, false, "", false, false},
	{ResCertificates, false, "", false, false},
	{ResFirewall, false, "", false, false},
	{ResSettings, false, "", false, false},
	{ResAudit, false, "", false, false},
}

func resourceInfo(r Resource) (ResourceInfo, bool) {
	i := slices.IndexFunc(Resources, func(info ResourceInfo) bool { return info.Resource == r })
	if i < 0 {
		return ResourceInfo{}, false
	}
	return Resources[i], true
}

// Level is how much of a resource a grant reaches.
type Level string

const (
	LevelNone   Level = "none"
	LevelView   Level = "view"
	LevelManage Level = "manage"
)

func (l Level) Valid() bool { return l == LevelNone || l == LevelView || l == LevelManage }

func (l Level) rank() int {
	switch l {
	case LevelView:
		return 1
	case LevelManage:
		return 2
	}
	return 0
}

// Grant is one resource in a role.
type Grant struct {
	Resource Resource `json:"resource"`
	Level    Level    `json:"level"`
	// Secrets reads what somebody set: variables, credentials, files.
	Secrets bool `json:"secrets,omitempty"`
	// Shell opens a shell inside an app's container. **Its own switch,
	// and never implied by the others**: a shell reads every secret the
	// running process holds and changes whatever the container can, so
	// it is more than manage and more than secrets, and a grant written
	// before it existed must not start reaching it.
	Shell bool `json:"shell,omitempty"`
	// Items names which ones. **Null is every one, and an empty list is
	// none** — never omitted, or the two would read back the same, and
	// a grant whose items were all deleted would reach everything.
	Items []string `json:"items"`
}

// Policy is what a caller may do, by resource. A resource that is absent
// is none of it. A nil Policy is everything: an admin.
type Policy map[Resource]Grant

// NewPolicy is grants as a policy, dropping the ones that grant nothing.
func NewPolicy(grants []Grant) Policy {
	p := Policy{}
	for _, g := range grants {
		if g.Level.rank() > 0 {
			p[g.Resource] = g
		}
	}
	return p
}

// Grants lists a policy in the order of Resources.
func (p Policy) Grants() []Grant {
	out := []Grant{}
	for _, info := range Resources {
		if g, ok := p[info.Resource]; ok {
			out = append(out, g)
		}
	}
	return out
}

var (
	// ErrBadGrant is a grant naming a resource or a level there is not,
	// or items on a resource that has none.
	ErrBadGrant = errors.New("invalid grant")
	// ErrHidden is an item outside the caller's grant. Modules answer it
	// as their own not found: a project outside a role is not there.
	ErrHidden = errors.New("not found")
)

// ValidateGrants checks what a role editor sent.
func ValidateGrants(grants []Grant) error {
	seen := map[Resource]bool{}
	for _, g := range grants {
		info, ok := resourceInfo(g.Resource)
		if !ok {
			return fmt.Errorf("%w: no resource %q", ErrBadGrant, g.Resource)
		}
		if seen[g.Resource] {
			return fmt.Errorf("%w: %s is granted twice", ErrBadGrant, g.Resource)
		}
		seen[g.Resource] = true
		if !g.Level.Valid() {
			return fmt.Errorf("%w: level must be none, view or manage", ErrBadGrant)
		}
		if g.Items != nil && !info.Items {
			return fmt.Errorf("%w: %s cannot name items", ErrBadGrant, g.Resource)
		}
		if g.Secrets && !info.Secrets {
			return fmt.Errorf("%w: %s has no secrets", ErrBadGrant, g.Resource)
		}
		if g.Shell && !info.Shell {
			return fmt.Errorf("%w: %s has no shell", ErrBadGrant, g.Resource)
		}
	}
	return nil
}

// DefaultMemberPolicy is a member with no role: what the member role
// reached before roles existed, so an account nobody gave a role keeps
// exactly what it had.
func DefaultMemberPolicy() Policy {
	return NewPolicy([]Grant{
		{Resource: ResProjects, Level: LevelView, Secrets: true},
		{Resource: ResApps, Level: LevelManage, Secrets: true},
		{Resource: ResDomains, Level: LevelView},
		{Resource: ResDatabases, Level: LevelView},
		{Resource: ResStorage, Level: LevelView},
		{Resource: ResServers, Level: LevelView},
		{Resource: ResTemplates, Level: LevelView},
		{Resource: ResRegistry, Level: LevelView},
	})
}

// Intersect is what both policies allow. A nil one allows everything.
func Intersect(p, q Policy) Policy {
	if p == nil {
		return q
	}
	if q == nil {
		return p
	}
	out := Policy{}
	for r, a := range p {
		b, ok := q[r]
		if !ok {
			continue
		}
		g := Grant{Resource: r, Level: a.Level, Secrets: a.Secrets && b.Secrets, Shell: a.Shell && b.Shell}
		if b.Level.rank() < a.Level.rank() {
			g.Level = b.Level
		}
		switch {
		case a.Items == nil:
			g.Items = b.Items
		case b.Items == nil:
			g.Items = a.Items
		default:
			g.Items = []string{}
			for _, item := range a.Items {
				if slices.Contains(b.Items, item) {
					g.Items = append(g.Items, item)
				}
			}
		}
		if g.Level.rank() > 0 {
			out[r] = g
		}
	}
	return out
}

// Within reports whether p allows nothing q does not.
func (p Policy) Within(q Policy) bool {
	if q == nil {
		return true
	}
	if p == nil {
		return false
	}
	for r, a := range p {
		b := q[r]
		if a.Level.rank() > b.Level.rank() || (a.Secrets && !b.Secrets) || (a.Shell && !b.Shell) {
			return false
		}
		if b.Items == nil {
			continue
		}
		if a.Items == nil {
			return false
		}
		for _, item := range a.Items {
			if !slices.Contains(b.Items, item) {
				return false
			}
		}
	}
	return true
}

// Allow is the question every module asks: may caller reach resource at
// level — and, when item is not empty, that one of them.
//
// A level short is ErrForbidden; an item outside the grant is ErrHidden,
// which a module answers as its own not found.
func Allow(caller *User, r Resource, need Level, item string) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	policy := caller.Effective()
	if policy == nil {
		return nil
	}
	g, ok := policy[r]
	if !ok || g.Level.rank() < need.rank() {
		if ok && item != "" && g.Items != nil && !slices.Contains(g.Items, item) {
			return ErrHidden
		}
		return denied(fmt.Sprintf("forbidden: this needs %s on %s", need, r))
	}
	if item != "" && g.Items != nil && !slices.Contains(g.Items, item) {
		return ErrHidden
	}
	return nil
}

// AllowSecrets is Allow at view, plus the grant reading secrets.
func AllowSecrets(caller *User, r Resource, item string) error {
	if err := Allow(caller, r, LevelView, item); err != nil {
		return err
	}
	if policy := caller.Effective(); policy != nil && !policy[r].Secrets {
		return denied(fmt.Sprintf("forbidden: this reads secrets on %s", r))
	}
	return nil
}

// AllowShell is Allow at manage, plus the grant opening a shell. Manage
// as well, because a shell changes what it reaches, and a grant that can
// only look must not be one switch away from a root prompt.
func AllowShell(caller *User, r Resource, item string) error {
	if err := Allow(caller, r, LevelManage, item); err != nil {
		return err
	}
	if policy := caller.Effective(); policy != nil && !policy[r].Shell {
		return denied(fmt.Sprintf("forbidden: this opens a shell on %s", r))
	}
	return nil
}

// HasShell reports whether caller opens a shell on some of a resource.
func HasShell(caller *User, r Resource) bool {
	return CanAny(caller, r, LevelManage) && (caller.Effective() == nil || caller.Effective()[r].Shell)
}

// denied is ErrForbidden with the grant that was missing in its message.
type denied string

func (d denied) Error() string { return string(d) }
func (d denied) Unwrap() error { return ErrForbidden }

// Sees reports whether caller may see one item, or any when item is "".
func Sees(caller *User, r Resource, item string) bool {
	return Allow(caller, r, LevelView, item) == nil
}

// CanAny reports whether caller reaches level on at least some of a
// resource — what decides whether a tool or a screen is offered at all.
func CanAny(caller *User, r Resource, need Level) bool {
	if caller == nil {
		return false
	}
	policy := caller.Effective()
	if policy == nil {
		return true
	}
	g, ok := policy[r]
	return ok && g.Level.rank() >= need.rank() && (g.Items == nil || len(g.Items) > 0)
}

// HasSecrets reports whether caller reads secrets on some of a resource.
func HasSecrets(caller *User, r Resource) bool {
	return CanAny(caller, r, LevelView) && (caller.Effective() == nil || caller.Effective()[r].Secrets)
}

// Effective is the policy a check applies. **Nil only for an admin**: a
// member whose Policy was never set — built by hand rather than by
// authentication — is the member default, never everything.
func (u *User) Effective() Policy {
	if u == nil {
		return Policy{}
	}
	if u.Policy == nil && u.Role != RoleAdmin {
		return DefaultMemberPolicy()
	}
	return u.Policy
}
