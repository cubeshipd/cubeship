package project

import (
	"errors"
	"net/http"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one project as both the API and the MCP tools report it.
type Response struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Environments []string `json:"environments,omitempty"`
}

// EnvironmentResponse is one environment, likewise shared.
type EnvironmentResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func toResponses(projects []*Project) []Response {
	out := make([]Response, 0, len(projects))
	for _, p := range projects {
		out = append(out, Response{Slug: p.Slug, Name: p.Name})
	}
	return out
}

func toEnvironmentResponses(envs []*Environment) []EnvironmentResponse {
	out := make([]EnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		out = append(out, EnvironmentResponse{Slug: e.Slug, Name: e.Name})
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("POST /orgs/{orgSlug}/projects", auth(http.HandlerFunc(h.create)))
	mux.Handle("GET /orgs/{orgSlug}/projects", auth(http.HandlerFunc(h.list)))
	mux.Handle("PUT /orgs/{orgSlug}/projects/{projectSlug}/env", auth(http.HandlerFunc(h.setEnv)))
	mux.Handle("POST /orgs/{orgSlug}/projects/{projectSlug}/environments", auth(http.HandlerFunc(h.createEnvironment)))
	mux.Handle("GET /orgs/{orgSlug}/projects/{projectSlug}/environments", auth(http.HandlerFunc(h.listEnvironments)))
	mux.Handle("PUT /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.setEnvironmentEnv)))
	mux.Handle("DELETE /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}", auth(http.HandlerFunc(h.deleteEnvironment)))
}

// WriteError maps this module's domain errors onto status codes, falling
// through to org's for the authorization failures it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrEnvironmentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrEnvironmentExists), errors.Is(err, ErrEnvironmentHasApps):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrProductionUndeletable):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		org.WriteError(w, err)
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
	p, env, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), r.PathValue("orgSlug"), req.Slug, req.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, Response{Slug: p.Slug, Name: p.Name, Environments: []string{env.Slug}})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.List(r.Context(), user.FromContext(r.Context()), r.PathValue("orgSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(projects))
}

// envVarsRequest is the body every "set env" endpoint takes. The map
// replaces whatever was set before — there is no partial update.
type envVarsRequest struct {
	Vars envvar.Map `json:"vars"`
}

func (h *Handler) setEnv(w http.ResponseWriter, r *http.Request) {
	var req envVarsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.SetEnv(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), req.Vars); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	env, err := h.svc.CreateEnvironment(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), req.Slug, req.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, EnvironmentResponse{Slug: env.Slug, Name: env.Name})
}

func (h *Handler) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := h.svc.ListEnvironments(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEnvironmentResponses(envs))
}

func (h *Handler) setEnvironmentEnv(w http.ResponseWriter, r *http.Request) {
	var req envVarsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.SetEnvironmentEnv(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), r.PathValue("envSlug"), req.Vars); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.DeleteEnvironment(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), r.PathValue("envSlug")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
