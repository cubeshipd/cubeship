package dns

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// AccountResponse is one DNS provider as the API reports it.
//
// The credential is named, not returned: a client needs to say which
// secret a provider writes through, and nothing more than its label to
// say it with.
type AccountResponse struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	// ProviderName is the provider as a person calls it, so a client
	// does not keep its own table of labels that drifts out of step.
	ProviderName string `json:"provider_name"`
	CredentialID int64  `json:"credential_id"`
	// Label is the credential's, which is what a person picked it out
	// of a list by.
	Label string `json:"label"`
	// Username is the first half where the secret has one. Not a
	// secret: the secret is the other half, and no endpoint returns it.
	Username  string    `json:"username,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProviderResponse is one provider a DNS account can be created for,
// and what to ask for when a login is typed rather than picked. It
// exists so a form is built from what this release actually supports
// rather than from a copy of the list that drifts.
type ProviderResponse struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	// UsernameLabel is what to call the first field, and absent where
	// the secret is a single value — then there is no first field.
	UsernameLabel string `json:"username_label,omitempty"`
	PasswordLabel string `json:"password_label"`
	Hint          string `json:"hint"`
}

func toResponse(a *Account) AccountResponse {
	return AccountResponse{
		ID: a.ID, Provider: string(a.Provider), ProviderName: a.Provider.Name(),
		CredentialID: a.CredentialID, Label: a.Label, Username: a.Username,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

const accountPath = "/dns/{id}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	// A DNS provider is which API to speak and which stored credential
	// to speak it with. The secret is not here — it is a credential,
	// created, renamed and rotated at /credentials, and one may be
	// writing records and pulling images at once.
	r.Handle("GET /dns", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /dns", auth(http.HandlerFunc(h.connect)))
	// A literal segment beside {id}, which the mux prefers — and an id
	// is a number, so they could not collide anyway.
	r.Handle("GET /dns/providers", auth(http.HandlerFunc(h.providers)))
	r.Handle("PATCH "+accountPath, auth(http.HandlerFunc(h.repoint)))
	r.Handle("DELETE "+accountPath, auth(http.HandlerFunc(h.disconnect)))
	r.Handle("GET /dns/{id}/status", auth(http.HandlerFunc(h.status)))
	r.Handle("GET /dns/{id}/zones", auth(http.HandlerFunc(h.zones)))
	r.Handle("GET /dns/{id}/records", auth(http.HandlerFunc(h.records)))
	r.Handle("PUT /dns/{id}/records", auth(http.HandlerFunc(h.putRecord)))
	r.Handle("DELETE /dns/{id}/records", auth(http.HandlerFunc(h.deleteRecord)))
}

// WriteError maps this module's domain errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, ErrNotFound),
		errors.Is(err, ErrRecordNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrRecordTypeUnknown), errors.Is(err, ErrUnknownProvider),
		errors.Is(err, ErrTwoLogins), errors.Is(err, ErrCredentialRequired):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrAccountTaken), errors.Is(err, ErrInstanceDNS):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		// Everything about the credential itself — no such credential, a
		// label already taken — is the credential module's to phrase, so
		// its mapping answers rather than this one guessing at a status
		// for an error it did not raise.
		credential.WriteError(w, err)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts, err := h.svc.Accounts(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]AccountResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toResponse(a))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if err := user.Require(user.FromContext(r.Context()), manageRole); err != nil {
		WriteError(w, err)
		return
	}
	out := make([]ProviderResponse, 0, len(Providers()))
	for _, p := range Providers() {
		out = append(out, ProviderResponse{
			Provider: string(p), Name: p.Name(),
			UsernameLabel: p.UsernameLabel(),
			PasswordLabel: p.PasswordLabel(),
			Hint:          p.Hint(),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider     string `json:"provider"`
		CredentialID int64  `json:"credential_id"`
		// A login typed instead of picked. Creating the credential here
		// is what keeps one from being a prerequisite — see NewLogin.
		Label    string `json:"label"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	var login *NewLogin
	if req.Password != "" || req.Username != "" || req.Label != "" {
		login = &NewLogin{Label: req.Label, Username: req.Username, Password: req.Password}
	}

	ctx := r.Context()
	created, err := h.svc.Connect(ctx, user.FromContext(ctx),
		Account{Provider: Provider(req.Provider), CredentialID: req.CredentialID}, login)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) repoint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		CredentialID int64 `json:"credential_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.CredentialID == 0 {
		http.Error(w, ErrCredentialRequired.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	updated, err := h.svc.Repoint(ctx, user.FromContext(ctx), id, req.CredentialID)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := h.svc.Disconnect(ctx, user.FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	status, err := h.svc.Probe(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (h *Handler) zones(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	zones, err := h.svc.Zones(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, zones)
}

func (h *Handler) records(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	records, err := h.svc.Records(ctx, user.FromContext(ctx),
		id, r.URL.Query().Get("zone"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, records)
}

func (h *Handler) putRecord(w http.ResponseWriter, r *http.Request) {
	var req Record
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	if err := h.svc.PutRecord(ctx, user.FromContext(ctx),
		id, r.URL.Query().Get("zone"), req); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	if err := h.svc.DeleteRecord(ctx, user.FromContext(ctx),
		id, query.Get("zone"), query.Get("name"), query.Get("type")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathID reads the provider's id, answering 404 for anything that is
// not one: a path segment that is not a number names nothing, and that
// is the same answer as naming something that does not exist.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}
