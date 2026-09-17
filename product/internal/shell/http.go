package shell

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/node"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/user"

	"github.com/coder/websocket"
)

// PasswordTimeout is how long a root shell waits for the password it
// asks for. A person typing it, not a program.
var PasswordTimeout = 2 * time.Minute

// Handler serves sessions.
type Handler struct {
	svc   *Service
	audit Recorder
	// agent authenticates a machine's own credential. See node.Handler.
	agent func(http.Handler) http.Handler
}

func NewHandler(svc *Service, audit Recorder, agent func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, audit: audit, agent: agent}
}

// Routes mounts the two sessions and the door workers connect back
// through.
//
// **All internal.** The OpenAPI document describes requests and their
// responses, and a terminal is neither: it is a connection held open for
// as long as somebody types. See docs/design/product/shell.md.
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.HandleInternal("GET /apps/{project}/{env}/{name}/shell", auth(http.HandlerFunc(h.appShell)))
	r.HandleInternal("GET /nodes/{name}/shell", auth(http.HandlerFunc(h.hostShell)))
	r.HandleInternal("GET /nodes/agent/shell/{id}", h.agent(http.HandlerFunc(h.claim)))
}

func (h *Handler) appShell(w http.ResponseWriter, r *http.Request) {
	conn, caller, ok := h.accept(w, r)
	if !ok {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	ref, err := app.ParseReference(r.PathValue("project") + "/" + r.PathValue("env") + "/" + r.PathValue("name"))
	var t Target
	if err == nil {
		t, err = h.svc.AppTarget(ctx, caller, ref, r.URL.Query().Get("server"))
	}
	h.session(ctx, r, conn, caller, "/apps/{project}/{env}/{name}/shell", t, err)
}

func (h *Handler) hostShell(w http.ResponseWriter, r *http.Request) {
	conn, caller, ok := h.accept(w, r)
	if !ok {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	t, err := h.svc.HostTarget(ctx, caller, r.PathValue("name"))
	// Somebody signed in to the dashboard confirms who they are before a
	// root prompt opens. An API key is its own proof — it is a secret
	// the CLI holds, not a cookie a browser left signed in.
	if err == nil && caller.Key == nil {
		wait, stop := context.WithTimeout(ctx, PasswordTimeout)
		m, rerr := terminal.ReadMessage(wait, conn)
		stop()
		switch {
		case rerr != nil:
			conn.CloseNow()
			return
		case m.Type != terminal.TypeAuth:
			err = ErrPasswordRequired
		default:
			err = h.svc.ConfirmPassword(ctx, caller, m.Password)
		}
	}
	h.session(ctx, r, conn, caller, "/nodes/{name}/shell", t, err)
}

// accept upgrades the request, once the caller is known to be allowed
// to try.
//
// **The origin is checked by hand for a signed-in browser.** A WebSocket
// is opened with a GET, which httpx.SameOrigin lets through because a
// GET changes nothing — and this one opens a shell. An app deployed on
// this instance is same-site with the dashboard, so its pages carry the
// session cookie; without this, any of them could open a shell as
// whoever visited.
func (h *Handler) accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, *user.User, bool) {
	caller := user.FromContext(r.Context())
	if caller == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, nil, false
	}
	if caller.Key == nil && !sameOrigin(r) {
		http.Error(w, "forbidden: this shell was opened from another site", http.StatusForbidden)
		return nil, nil, false
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, nil, false
	}
	return conn, caller, true
}

// session records the attempt, runs it when it was allowed, and records
// how it ended.
func (h *Handler) session(ctx context.Context, r *http.Request, conn *websocket.Conn, caller *user.User, route string, t Target, err error) {
	via := audit.ViaDashboard
	if caller.Key != nil {
		via = audit.ViaAPI
	}
	event := func(action string) audit.Event {
		e := audit.From(caller, via, audit.ClientIP(r))
		e.Action = action + " " + route
		e.Target = r.URL.Path[len(httpx.APIPrefix):]
		return e
	}

	open := event("GET")
	if err != nil {
		open.Outcome, open.Status, open.Detail = audit.OutcomeFailed, http.StatusConflict, Message(err)
		if errors.Is(err, user.ErrForbidden) || errors.Is(err, user.ErrInvalidCredentials) || errors.Is(err, ErrPasswordRequired) {
			open.Outcome, open.Status = audit.OutcomeRefused, http.StatusForbidden
		}
		h.audit.Record(ctx, open)
		terminal.Fail(ctx, conn, errors.New(Message(err)))
		return
	}
	open.Outcome, open.Status, open.Detail = audit.OutcomeOK, http.StatusSwitchingProtocols, t.Label
	h.audit.Record(ctx, open)

	started := time.Now()
	out := h.svc.Run(ctx, conn, t, size(r))

	closed := event("CLOSE")
	closed.Outcome, closed.Status = audit.OutcomeOK, http.StatusOK
	closed.Detail = describe(out, time.Since(started))
	if out.Reason == terminal.ReasonFailed {
		closed.Outcome = audit.OutcomeFailed
	}
	h.audit.Record(ctx, closed)
}

// claim is a worker connecting back with the session it was told to
// open.
func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	from := node.AgentFromContext(r.Context())
	if from == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	if err := h.svc.Claim(context.WithoutCancel(r.Context()), from, r.PathValue("id"), conn); err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
	}
}

// describe is how a session ended, in the words the log keeps.
func describe(out terminal.Outcome, lasted time.Duration) string {
	d := lasted.Round(time.Second).String()
	switch out.Reason {
	case terminal.ReasonExited:
		return "exited with " + strconv.Itoa(out.Code) + " after " + d
	case terminal.ReasonIdle:
		return "closed for inactivity after " + d
	case terminal.ReasonFailed:
		if out.Err != nil {
			return out.Err.Error()
		}
		return "failed after " + d
	default:
		return "closed after " + d
	}
}

// size is the terminal size the client asked for, or the default.
func size(r *http.Request) terminal.Size {
	cols, _ := strconv.ParseUint(r.URL.Query().Get("cols"), 10, 16)
	rows, _ := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 16)
	s := terminal.Size{Cols: uint16(cols), Rows: uint16(rows)}
	if !s.Valid() {
		return terminal.DefaultSize
	}
	return s
}

// sameOrigin is httpx.SameOrigin without its exemption for a GET.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	probe := r.Clone(r.Context())
	probe.Method = http.MethodPost
	return httpx.SameOrigin(probe)
}
