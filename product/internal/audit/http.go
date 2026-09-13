package audit

import (
	"bytes"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one event as the API and the MCP tool return it.
type Response struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Username string    `json:"username"`
	Via      Via       `json:"via"`
	KeyName  string    `json:"key_name,omitempty"`
	Action   string    `json:"action"`
	Target   string    `json:"target,omitempty"`
	Outcome  Outcome   `json:"outcome"`
	Status   int       `json:"status,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	IP       string    `json:"ip,omitempty"`
}

// Page is one page of the log. Next is the before to ask for the page
// after it, absent on the last one.
type Page struct {
	Events []Response `json:"events"`
	Next   int64      `json:"next,omitempty"`
}

func toPage(events []*Event, limit int) Page {
	p := Page{Events: make([]Response, 0, len(events))}
	for _, e := range events {
		p.Events = append(p.Events, Response{
			ID: e.ID, At: e.At, Username: e.Username, Via: e.Via, KeyName: e.KeyName,
			Action: e.Action, Target: e.Target, Outcome: e.Outcome, Status: e.Status,
			Detail: e.Detail, IP: e.IP,
		})
	}
	if limit > 0 && len(events) == limit {
		p.Next = events[len(events)-1].ID
	}
	return p
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /audit", auth(http.HandlerFunc(h.list)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{
		Username: q.Get("user"),
		Via:      Via(q.Get("via")),
		Outcome:  Outcome(q.Get("outcome")),
		Target:   q.Get("target"),
	}
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	f.Limit = min(f.Limit, MaxLimit)
	ctx := r.Context()
	events, err := h.svc.List(ctx, user.FromContext(ctx), f)
	if err != nil {
		user.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPage(events, f.Limit))
}

// quiet are changes nobody needs to read back: marking release notes
// read.
var quiet = map[string]bool{
	"POST /releases/seen": true,
}

// Record wraps an authenticated route and writes down what it changed.
// It sits inside authentication, so a caller is always known, and
// outside the key policy, so a refusal is recorded too.
//
// A read is recorded only when it was refused: a key trying to read a
// secret is worth knowing about, and a thousand listings are not.
func (h *Handler) Record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		action := strings.Replace(r.Pattern, httpx.APIPrefix+"/", "/", 1)
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		status := rec.code()
		if quiet[action] || (read && status != http.StatusForbidden) {
			return
		}
		caller := user.FromContext(r.Context())
		via := ViaDashboard
		if caller != nil && caller.Key != nil {
			via = ViaAPI
		}
		e := From(caller, via, ClientIP(r))
		e.Action = action
		e.Target = strings.TrimPrefix(r.URL.Path, httpx.APIPrefix)
		e.Status = status
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			e.Outcome = OutcomeRefused
		case status >= 400:
			e.Outcome = OutcomeFailed
		default:
			e.Outcome = OutcomeOK
		}
		if status >= 400 {
			e.Detail = rec.body.String()
		}
		h.svc.Record(r.Context(), e)
	})
}

// recorder keeps the status and the start of an error body.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status >= 400 && r.body.Len() < maxDetail {
		r.body.Write(b[:min(len(b), maxDetail-r.body.Len())])
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// Unwrap lets http.ResponseController reach the writer underneath, so a
// streamed response still flushes.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ClientIP is the address the request came from: the forwarded one
// behind Traefik, the socket's otherwise. A forged header is written
// down as sent, which is what an address in a log can promise.
func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return ""
}
