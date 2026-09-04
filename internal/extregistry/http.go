package extregistry

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/org"
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
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	Host     string `json:"host"`
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
		ID: c.ID, Provider: string(c.Provider), Host: c.Host,
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
	r.Handle("GET /orgs/{orgSlug}/registries", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /orgs/{orgSlug}/registries", auth(http.HandlerFunc(h.create)))
	r.Handle("PUT /orgs/{orgSlug}/registries/{id}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /orgs/{orgSlug}/registries/{id}", auth(http.HandlerFunc(h.delete)))
	r.Handle("GET /orgs/{orgSlug}/registries/{id}/repositories", auth(http.HandlerFunc(h.repositories)))
	r.Handle("GET /orgs/{orgSlug}/registries/{id}/images", auth(http.HandlerFunc(h.images)))
	r.Handle("GET /orgs/{orgSlug}/registries/{id}/status", auth(http.HandlerFunc(h.status)))
	r.Handle("GET /orgs/{orgSlug}/registries/{id}/usage", auth(http.HandlerFunc(h.usage)))
	r.Handle("DELETE /orgs/{orgSlug}/registries/{id}/images", auth(http.HandlerFunc(h.deleteImage)))
	r.Handle("DELETE /orgs/{orgSlug}/registries/{id}/repositories", auth(http.HandlerFunc(h.deleteRepository)))
}

// WriteError maps this module's domain errors onto status codes. The
// 404/403 split is org.Service.Authorize's, unchanged.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, org.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, org.ErrNotFound), errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrNoListing):
		http.Error(w, err.Error(), http.StatusNotImplemented)
	case errors.Is(err, ErrHostTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrHostRequired),
		errors.Is(err, ErrUsernameRequired), errors.Is(err, ErrPasswordRequired),
		errors.Is(err, ErrNamespaceRequired), errors.Is(err, ErrRegionRequired):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.List(ctx, user.FromContext(ctx), r.PathValue("orgSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(creds))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		// Host is the registry, for a generic one. Fixed for
		// DigitalOcean and discovered for AWS, so it is ignored there.
		Host string `json:"host"`
		// Namespace is DigitalOcean's registry name: what follows
		// registry.digitalocean.com/ in an image path.
		Namespace string `json:"namespace"`
		// Region is AWS's. An ECR registry lives in one and it cannot
		// be guessed.
		Region string `json:"region"`
		// Username is the login's user, or an AWS access key id;
		// Password its password, token, or secret access key.
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	created, err := h.svc.Create(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), Credential{
		Provider:  Provider(req.Provider),
		Host:      req.Host,
		Namespace: req.Namespace,
		Region:    req.Region,
		Username:  req.Username,
		Password:  req.Password,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// A pointer, because omitting it means "leave the name alone"
		// and sending "" means "you gave me nothing" — two different
		// requests that a plain string cannot tell apart.
		Namespace *string `json:"namespace"`
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
	updated, err := h.svc.Update(ctx, user.FromContext(ctx), r.PathValue("orgSlug"),
		id, req.Username, req.Password, req.Namespace)
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
	repos, err := h.svc.Repositories(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id)
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
	images, err := h.svc.Images(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id, repository)
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
	usage, err := h.svc.Usage(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id)
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
	if err := h.svc.DeleteImage(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id,
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
	if err := h.svc.DeleteRepository(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id,
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
	status, err := h.svc.Probe(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id)
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
	if err := h.svc.Delete(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
