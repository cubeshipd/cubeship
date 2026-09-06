package extregistry

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one credential as the API returns it.
//
// There is no password field, and that is not an oversight: a stored
// password exists to be sent to a registry, and an endpoint that hands
// it back turns every read of this list into a way to exfiltrate it.
// Replace it if you need to change it.
type Response struct {
	ID int64 `json:"id"`
	// CredentialID is the stored account this authenticates as, and
	// where its secret lives. Provider comes from that account rather
	// than being a second copy of it.
	CredentialID int64  `json:"credential_id"`
	Provider     string `json:"provider"`
	Host         string `json:"host"`
	// Namespace is the path segment between the host and the image
	// where the provider has one. Absent otherwise.
	Namespace string `json:"namespace,omitempty"`
	Region    string `json:"region,omitempty"`
	// Username is the login's user, or an AWS access key id. Never the
	// secret, whichever it is.
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(c *Credential) Response {
	return Response{
		ID: c.ID, CredentialID: c.CredentialID,
		Provider: string(c.Provider), Host: c.Host,
		Namespace: c.Namespace, Region: c.Region, Username: c.Username,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toResponses(creds []*Credential) []Response {
	out := make([]Response, 0, len(creds))
	for _, c := range creds {
		out = append(out, toResponse(c))
	}
	return out
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /registries", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /registries", auth(http.HandlerFunc(h.create)))
	r.Handle("PUT /registries/{id}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /registries/{id}", auth(http.HandlerFunc(h.delete)))
	r.Handle("GET /registries/{id}/repositories", auth(http.HandlerFunc(h.repositories)))
	r.Handle("GET /registries/{id}/images", auth(http.HandlerFunc(h.images)))
	r.Handle("GET /registries/{id}/status", auth(http.HandlerFunc(h.status)))
	r.Handle("GET /registries/{id}/usage", auth(http.HandlerFunc(h.usage)))
	r.Handle("DELETE /registries/{id}/images", auth(http.HandlerFunc(h.deleteImage)))
	r.Handle("DELETE /registries/{id}/repositories", auth(http.HandlerFunc(h.deleteRepository)))
}

// WriteError maps this module's domain errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrNoListing):
		http.Error(w, err.Error(), http.StatusNotImplemented)
	case errors.Is(err, ErrHostTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrTwoLogins):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrHostRequired),
		errors.Is(err, ErrUsernameRequired), errors.Is(err, ErrPasswordRequired),
		errors.Is(err, ErrNamespaceRequired), errors.Is(err, ErrRegionRequired):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		// A login typed here is stored as a credential, so the refusals
		// about a secret — a name where the provider has none, a label
		// already taken — are that module's to phrase rather than this
		// one guessing at a status for an error it did not raise.
		credential.WriteError(w, err)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.List(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(creds))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// CredentialID is the stored account this registry
		// authenticates as. It decides the provider, so nothing else
		// need name one.
		CredentialID int64 `json:"credential_id"`
		// Or a login typed here, for somebody who has no stored account
		// yet — the account is created from it and is there to pick
		// next time. Then the provider is asked for, because there is
		// no account to read it off.
		Provider string `json:"provider"`
		Label    string `json:"label"`
		Username string `json:"username"`
		Password string `json:"password"`
		// Host is the registry, for a generic one. Fixed for
		// DigitalOcean and discovered for AWS, so it is ignored there.
		Host string `json:"host"`
		// Namespace is DigitalOcean's registry name: what follows
		// registry.digitalocean.com/ in an image path.
		Namespace string `json:"namespace"`
		// Region is AWS's. An ECR registry lives in one and it cannot
		// be guessed.
		Region string `json:"region"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// A login is offered when any of its parts is: a request that names
	// a provider and no secret is somebody who meant to type one, and
	// telling them the secret is missing beats "no login" for a form
	// they just filled in.
	var login *NewLogin
	if req.Provider != "" || req.Username != "" || req.Password != "" {
		login = &NewLogin{
			Provider: Provider(req.Provider),
			Label:    req.Label,
			Username: req.Username,
			Password: req.Password,
		}
	}

	ctx := r.Context()
	created, err := h.svc.Create(ctx, user.FromContext(ctx), Credential{
		CredentialID: req.CredentialID,
		Host:         req.Host,
		Namespace:    req.Namespace,
		Region:       req.Region,
	}, login)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// All pointers, because omitting one means "leave it alone"
		// and sending it means "change it" — two different requests
		// that a plain value cannot tell apart.
		//
		// A login here rotates the account this registry authenticates
		// as, so every registry on that account follows. Pointing this
		// one somewhere else instead is credential_id.
		CredentialID *int64  `json:"credential_id"`
		Namespace    *string `json:"namespace"`
		Username     *string `json:"username"`
		Password     *string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	updated, err := h.svc.Update(ctx, user.FromContext(ctx), id, Changes{
		CredentialID: req.CredentialID,
		Namespace:    req.Namespace,
		Username:     req.Username,
		Password:     req.Password,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated))
}

// repositories lists what a registry holds. Not every registry can say:
// Docker Hub and GitHub's disable the catalogue endpoint the Registry v2
// API defines, and 501 says that rather than answering with an empty
// list that reads as "you have nothing".
func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	repos, err := h.svc.Repositories(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, repos)
}

// images lists one repository's tags, named in the query.
func (h *Handler) images(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	repository := r.URL.Query().Get("repository")
	if repository == "" {
		http.Error(w, "name the repository with ?repository=", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	images, err := h.svc.Images(ctx, user.FromContext(ctx), id, repository)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, images)
}

// usage measures what the images add up to. Its own endpoint because it
// is one call per repository: a listing that waited for this would wait
// for all of them.
func (h *Handler) usage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	usage, err := h.svc.Usage(ctx, user.FromContext(ctx), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, usage)
}

func (h *Handler) deleteImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.DeleteImage(ctx, user.FromContext(ctx), id,
		r.URL.Query().Get("repository"), ImageRef{
			Tag:    r.URL.Query().Get("tag"),
			Digest: r.URL.Query().Get("digest"),
		}); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteRepository(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.DeleteRepository(ctx, user.FromContext(ctx), id,
		r.URL.Query().Get("repository")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
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

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.Delete(ctx, user.FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
