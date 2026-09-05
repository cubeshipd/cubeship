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
	Description  string   `json:"description"`
	Environments []string `json:"environments,omitempty"`
}

// EnvironmentResponse is one environment, likewise shared.
type EnvironmentResponse struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

func toResponses(projects []*Project) []Response {
	out := make([]Response, 0, len(projects))
	for _, p := range projects {
		out = append(out, toResponse(p))
	}
	return out
}

func toResponse(p *Project) Response {
	return Response{Slug: p.Slug, Description: p.Description}
}

func toEnvironmentResponses(envs []*Environment) []EnvironmentResponse {
	out := make([]EnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		out = append(out, toEnvironmentResponse(e))
	}
	return out
}

func toEnvironmentResponse(e *Environment) EnvironmentResponse {
	return EnvironmentResponse{Slug: e.Slug, Description: e.Description}
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /orgs/{orgSlug}/projects", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /orgs/{orgSlug}/projects", auth(http.HandlerFunc(h.list)))
	r.Handle("PATCH /orgs/{orgSlug}/projects/{projectSlug}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /orgs/{orgSlug}/projects/{projectSlug}", auth(http.HandlerFunc(h.delete)))
	r.Handle("GET /orgs/{orgSlug}/projects/{projectSlug}/env", auth(http.HandlerFunc(h.getEnv)))
	r.Handle("PUT /orgs/{orgSlug}/projects/{projectSlug}/env", auth(http.HandlerFunc(h.setEnv)))
	r.Handle("PATCH /orgs/{orgSlug}/projects/{projectSlug}/env", auth(http.HandlerFunc(h.mergeEnv)))
	r.Handle("POST /orgs/{orgSlug}/projects/{projectSlug}/environments", auth(http.HandlerFunc(h.createEnvironment)))
	r.Handle("GET /orgs/{orgSlug}/projects/{projectSlug}/environments", auth(http.HandlerFunc(h.listEnvironments)))
	r.Handle("GET /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.getEnvironmentEnv)))
	r.Handle("PUT /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.setEnvironmentEnv)))
	r.Handle("PATCH /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.mergeEnvironmentEnv)))
	r.Handle("PATCH /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}", auth(http.HandlerFunc(h.updateEnvironment)))
	r.Handle("DELETE /orgs/{orgSlug}/projects/{projectSlug}/environments/{envSlug}", auth(http.HandlerFunc(h.deleteEnvironment)))
}

// WriteError maps this module's domain errors onto status codes, falling
// through to org's for the authorization failures it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrEnvironmentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrEnvironmentExists),
		errors.Is(err, ErrEnvironmentHasApps), errors.Is(err, ErrHasApps):
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
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}
	p, env, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), r.PathValue("orgSlug"), req.Slug)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, Response{Slug: p.Slug, Description: p.Description, Environments: []string{env.Slug}})
}

// update is PATCH: a field left out of the body is left alone, which is
// what lets the dashboard save one edit without sending the other back.
// The slug is not a field — see Service.Update.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Description == nil {
		http.Error(w, "give a description", http.StatusBadRequest)
		return
	}
	p, err := h.svc.Update(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), req.Description)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(p))
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

// EnvResponse is what reading variables at one level returns. Effective
// is empty for a project, which inherits from nothing.
type EnvResponse struct {
	Vars      envvar.Map        `json:"vars"`
	Effective []envvar.Resolved `json:"effective,omitempty"`
}

// MergeEnvRequest adds or overwrites the variables in set and removes
// those named in unset, leaving everything else alone.
type MergeEnvRequest struct {
	Set   envvar.Map `json:"set"`
	Unset []string   `json:"unset"`
}

// delete removes a project and its environments, refusing while any app
// remains.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.Delete(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getEnv(w http.ResponseWriter, r *http.Request) {
	vars, err := h.svc.Env(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, EnvResponse{Vars: vars})
}

func (h *Handler) mergeEnv(w http.ResponseWriter, r *http.Request) {
	var req MergeEnvRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.MergeEnv(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), req.Set, req.Unset); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getEnvironmentEnv(w http.ResponseWriter, r *http.Request) {
	vars, effective, err := h.svc.EnvironmentEnv(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), r.PathValue("envSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, EnvResponse{Vars: vars, Effective: effective})
}

func (h *Handler) mergeEnvironmentEnv(w http.ResponseWriter, r *http.Request) {
	var req MergeEnvRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.MergeEnvironmentEnv(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), r.PathValue("envSlug"),
		req.Set, req.Unset); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
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
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}
	env, err := h.svc.CreateEnvironment(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), req.Slug)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toEnvironmentResponse(env))
}

// updateEnvironment is PATCH: a field left out of the body is left
// alone. The slug is not among them — it is the third component of
// every app reference in the environment.
func (h *Handler) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Description == nil {
		http.Error(w, "give a description", http.StatusBadRequest)
		return
	}
	env, err := h.svc.UpdateEnvironment(r.Context(), user.FromContext(r.Context()),
		r.PathValue("orgSlug"), r.PathValue("projectSlug"), r.PathValue("envSlug"),
		req.Description)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEnvironmentResponse(env))
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
