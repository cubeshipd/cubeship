package org

import (
	"errors"
	"net/http"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Response is one organization as the API reports it — shared with the
// MCP tools, so both surfaces describe an organization identically.
type Response struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func toResponses(orgs []*Organization) []Response {
	out := make([]Response, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, Response{Slug: o.Slug, Name: o.Name})
	}
	return out
}

// CreateUserResponse is the result of adding a user to an organization.
type CreateUserResponse struct {
	Username string `json:"username"`
	Org      string `json:"org"`
	Role     string `json:"role"`
	// APIKey is the new user's key, shown exactly once. It is omitted
	// when an existing user was added to a further organization — that
	// user keeps the key they already have.
	APIKey string `json:"api_key,omitempty"`
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /orgs", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /orgs", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /orgs/{orgSlug}/users", auth(http.HandlerFunc(h.createUser)))
}

// WriteError maps this module's domain errors onto status codes. Other
// modules reuse it, since they surface the same authorization failures
// when resolving the organization a resource belongs to.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrSuperAdminOnly):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrAlreadyMember):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrInvalidRole):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, slug.ErrInvalid):
		http.Error(w, "slug "+slug.ErrInvalid.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), req.Slug, req.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, Response{Slug: created.Slug, Name: created.Name})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(orgs))
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller := user.FromContext(ctx)

	o, err := h.svc.Resolve(ctx, caller, r.PathValue("orgSlug"), RoleAdmin)
	if err != nil {
		WriteError(w, err)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	apiKey, err := h.svc.AddUser(ctx, caller, o, req.Username, Role(req.Role))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, CreateUserResponse{
		Username: req.Username, Org: o.Slug, Role: req.Role, APIKey: apiKey,
	})
}
