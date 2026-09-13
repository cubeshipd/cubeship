// Package audit is who changed what on this instance, and through which
// door: the dashboard, the API, or an agent over MCP.
//
// It records at the two doors rather than inside each module, so a
// route or a tool added later is recorded without anybody remembering
// to. What it cannot see is what no person asked for in that request —
// a push webhook's deploy, a scheduled backup.
package audit

import (
	"time"

	"cubeship/internal/user"
)

// Via is the door a change came through.
type Via string

const (
	ViaDashboard Via = "dashboard"
	ViaAPI       Via = "api"
	ViaMCP       Via = "mcp"
)

// Outcome is how it ended.
type Outcome string

const (
	OutcomeOK Outcome = "ok"
	// OutcomeRefused is a request the caller was not allowed: a role, a
	// key's access, a project outside a key's scope.
	OutcomeRefused Outcome = "refused"
	OutcomeFailed  Outcome = "failed"
)

// Event is one recorded change, or one refused attempt at one.
type Event struct {
	ID       int64
	At       time.Time
	UserID   int64
	Username string
	Via      Via
	KeyID    int64
	KeyName  string
	// Action is the route pattern or the tool: "POST /apps", "mcp deploy_app".
	Action string
	// Target is what it named: the path, or a tool's identifying arguments.
	Target  string
	Outcome Outcome
	// Status is the HTTP status, 0 for a tool.
	Status int
	// Detail is the error the caller was given, cut short. Never a body
	// that was sent: those carry secrets.
	Detail string
	IP     string
}

// Filter narrows a listing. Before pages backwards by id.
type Filter struct {
	Username string
	Via      Via
	Outcome  Outcome
	// Target matches anywhere in the target, so an app's reference finds
	// every change to it.
	Target string
	Before int64
	Limit  int
}

const (
	// Retention is how long an event is kept.
	Retention = 90 * 24 * time.Hour
	// DefaultLimit and MaxLimit bound one page.
	DefaultLimit = 100
	MaxLimit     = 500
	// maxDetail is how much of an error is kept.
	maxDetail = 300
)

// From starts an event for caller.
func From(caller *user.User, via Via, ip string) Event {
	e := Event{Via: via, IP: ip}
	if caller == nil {
		return e
	}
	e.UserID, e.Username = caller.ID, caller.Username
	if caller.Key != nil {
		e.KeyID, e.KeyName = caller.Key.ID, caller.Key.Name
	}
	return e
}
