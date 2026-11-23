package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"cubeship/internal/audit"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// DeployHookResponse is an app's webhook as the API reports it. URL is
// null while the instance has no domain.
type DeployHookResponse struct {
	URL   *string `json:"url"`
	Token string  `json:"token"`
}

func toDeployHookResponse(h *DeployHook) DeployHookResponse {
	r := DeployHookResponse{Token: h.Token}
	if h.URL != "" {
		r.URL = &h.URL
	}
	return r
}

// HookDeployResponse is what a webhook call answers: short, because it
// is read by a CI log, and without anything about the app beyond its
// name.
type HookDeployResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	App    string `json:"app"`
	Tag    string `json:"tag,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Recorder writes the audit log, for the one route here that no person
// is signed in to.
type Recorder interface {
	Record(ctx context.Context, e audit.Event)
}

// HookAction is how a webhook deploy reads in the audit log. A pattern
// of its own rather than the route's, which carries the token.
const HookAction = "HOOK " + appPath + "/deploy"

// HookActor is who the audit log says did it.
const HookActor = "deploy webhook"

func (h *Handler) getDeployHook(w http.ResponseWriter, r *http.Request) {
	hook, err := h.svc.DeployHook(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeployHookResponse(hook))
}

func (h *Handler) rotateDeployHook(w http.ResponseWriter, r *http.Request) {
	hook, err := h.svc.RotateDeployHook(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeployHookResponse(hook))
}

// WebhookRoutes mounts the deploy webhook outside the API, where a CI
// can reach it with nothing but the URL. POST only: a GET would deploy
// whenever a chat app unfurled a pasted link.
func (h *Handler) WebhookRoutes(r *httpx.Router, rec Recorder) {
	r.HandleRootFunc("POST "+HookPath+"{token}", func(w http.ResponseWriter, r *http.Request) {
		h.hook(w, r, rec)
	})
}

// hookBodyLimit is how much of a webhook's body is read before it is
// thrown away.
const hookBodyLimit = 1 << 20

func (h *Handler) hook(w http.ResponseWriter, r *http.Request, rec Recorder) {
	// The body is never read for what it says. GitHub, Docker Hub and
	// every other sender post a JSON document of their own, whatever
	// the URL is for, and a hook that parsed it would either refuse
	// them or take one of their fields for one of ours. It is drained,
	// within a bound, so the connection can be reused.
	io.Copy(io.Discard, io.LimitReader(r.Body, hookBodyLimit))

	req, err := hookRequestFrom(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.svc.DeployFromHook(r.Context(), r.PathValue("token"), req)
	if result == nil {
		// An unknown token, answered as any other address nothing is
		// served at, and not recorded: it names no app to record it
		// against, and a scan would fill the log.
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
		} else {
			WriteError(w, err)
		}
		return
	}

	e := audit.Event{Username: HookActor, Via: audit.ViaWebhook, IP: audit.ClientIP(r), Action: HookAction}
	e.Target = "/apps/" + result.App.String() + "/deploy"
	waitEnded := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
	if err != nil && !waitEnded {
		rw := &statusWriter{ResponseWriter: w}
		WriteError(rw, err)
		e.Outcome, e.Status, e.Detail = audit.OutcomeFailed, rw.status, err.Error()
		rec.Record(r.Context(), e)
		return
	}

	d := result.Deployment
	status := http.StatusAccepted
	if req.Wait && !waitEnded {
		status = http.StatusOK
	}
	e.Outcome, e.Status = audit.OutcomeOK, status
	e.Detail = strings.TrimSpace(fmt.Sprintf("deployment %d %s", d.ID, hookVersion(result)))
	rec.Record(r.Context(), e)
	httpx.WriteJSON(w, status, HookDeployResponse{
		ID: d.ID, Status: d.Status, App: result.App.String(),
		Tag: result.Tag, Ref: result.Ref, Error: d.Error,
	})
}

// hookRequestFrom reads the query string. A key it does not know is
// refused rather than ignored: ?tags=1.2.3 would otherwise deploy the
// tag the app is already on and answer as if it had done what was asked.
func hookRequestFrom(q url.Values) (HookRequest, error) {
	var req HookRequest
	for key := range q {
		switch key {
		case "tag":
			req.Tag = q.Get(key)
		case "ref":
			req.Ref = q.Get(key)
		case "wait":
			wait, err := strconv.ParseBool(q.Get(key))
			if err != nil {
				return HookRequest{}, fmt.Errorf("wait is true or false, not %q", q.Get(key))
			}
			req.Wait = wait
		default:
			return HookRequest{}, fmt.Errorf("unknown parameter %q: the webhook takes tag, ref and wait", key)
		}
	}
	return req, nil
}

func hookVersion(r *HookResult) string {
	switch {
	case r.Tag != "":
		return "tag " + r.Tag
	case r.Ref != "":
		return "ref " + r.Ref
	}
	return ""
}

// statusWriter remembers the status WriteError chose, for the log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
