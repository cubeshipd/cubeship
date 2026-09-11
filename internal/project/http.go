package project

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// Response is one project as both the API and the MCP tools report it.
type Response struct {
	Slug         string   `json:"slug"`
	Environments []string `json:"environments,omitempty"`
	// HasImage says whether this project wears a picture, so a grid can
	// ask for one or draw its own mark without a request per project
	// that mostly 404s.
	HasImage bool `json:"has_image,omitempty"`
}

// EnvironmentResponse is one environment, likewise shared.
type EnvironmentResponse struct {
	Slug string `json:"slug"`
}

func toResponses(projects []*Project) []Response {
	out := make([]Response, 0, len(projects))
	for _, p := range projects {
		out = append(out, toResponse(p))
	}
	return out
}

func toResponse(p *Project) Response {
	return Response{Slug: p.Slug, HasImage: p.HasImage()}
}

func toEnvironmentResponses(envs []*Environment) []EnvironmentResponse {
	out := make([]EnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		out = append(out, toEnvironmentResponse(e))
	}
	return out
}

func toEnvironmentResponse(e *Environment) EnvironmentResponse {
	return EnvironmentResponse{Slug: e.Slug}
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /projects", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /projects", auth(http.HandlerFunc(h.list)))
	r.Handle("DELETE /projects/{projectSlug}", auth(http.HandlerFunc(h.delete)))
	r.Handle("GET /projects/{projectSlug}/image", auth(http.HandlerFunc(h.image)))
	r.Handle("PUT /projects/{projectSlug}/image", auth(http.HandlerFunc(h.setImage)))
	r.Handle("DELETE /projects/{projectSlug}/image", auth(http.HandlerFunc(h.clearImage)))
	r.Handle("GET /projects/{projectSlug}/env", auth(http.HandlerFunc(h.getEnv)))
	r.Handle("PUT /projects/{projectSlug}/env", auth(http.HandlerFunc(h.setEnv)))
	r.Handle("PATCH /projects/{projectSlug}/env", auth(http.HandlerFunc(h.mergeEnv)))
	r.Handle("POST /projects/{projectSlug}/environments", auth(http.HandlerFunc(h.createEnvironment)))
	r.Handle("GET /projects/{projectSlug}/environments", auth(http.HandlerFunc(h.listEnvironments)))
	r.Handle("GET /projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.getEnvironmentEnv)))
	r.Handle("PUT /projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.setEnvironmentEnv)))
	r.Handle("PATCH /projects/{projectSlug}/environments/{envSlug}/env", auth(http.HandlerFunc(h.mergeEnvironmentEnv)))
	r.Handle("DELETE /projects/{projectSlug}/environments/{envSlug}", auth(http.HandlerFunc(h.deleteEnvironment)))
}

// WriteError maps this module's domain errors onto status codes, falling
// through to org's for the authorization failures it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrEnvironmentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrEnvironmentExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrProductionUndeletable):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrNoImage):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrImageType):
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, ErrImageTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	default:
		user.WriteError(w, err)
	}
}

// image serves a project's picture.
//
// **Cached against the media type rather than for a fixed time.** A
// picture is drawn on the projects grid, so without a cache header it
// is a request per project on every visit; with a fixed one, replacing
// it leaves the old one on screen until that expires. `no-cache` is the
// pair that gets both: the browser keeps the bytes and asks every time
// whether they are still current, and the answer is a 304 with no body.
func (h *Handler) image(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, mediaType, err := h.svc.Image(ctx, user.FromContext(ctx), r.PathValue("projectSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-cache")
	// The bytes are their own version: replacing the picture changes
	// the tag, and nothing else has to be invalidated by hand.
	w.Header().Set("ETag", `"`+fmt.Sprintf("%x", sha256.Sum256(data))[:16]+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// setImage replaces the picture, from the raw body.
//
// **Raw rather than multipart**, because there is one field and nothing
// else: what the dashboard sends is a Blob it has already scaled, and a
// multipart envelope around a single value is a form where there is no
// form. The header it arrives with decides nothing — see
// Service.SetImage.
func (h *Handler) setImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.svc.SetImage(ctx, user.FromContext(ctx), r.PathValue("projectSlug"), r.Body); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) clearImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.svc.ClearImage(ctx, user.FromContext(ctx), r.PathValue("projectSlug")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}
	p, env, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), req.Slug)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, Response{Slug: p.Slug, Environments: []string{env.Slug}})
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projects, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
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
	if _, err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getEnv(w http.ResponseWriter, r *http.Request) {
	vars, err := h.svc.Env(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"))
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
	if _, err := h.svc.MergeEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), req.Set, req.Unset); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getEnvironmentEnv(w http.ResponseWriter, r *http.Request) {
	vars, effective, err := h.svc.EnvironmentEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), r.PathValue("envSlug"))
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
	if _, err := h.svc.MergeEnvironmentEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), r.PathValue("envSlug"),
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
	if _, err := h.svc.SetEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), req.Vars); err != nil {
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
	env, err := h.svc.CreateEnvironment(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), req.Slug)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toEnvironmentResponse(env))
}
func (h *Handler) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := h.svc.ListEnvironments(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"))
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
	if _, err := h.svc.SetEnvironmentEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), r.PathValue("envSlug"), req.Vars); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.DeleteEnvironment(r.Context(), user.FromContext(r.Context()), r.PathValue("projectSlug"), r.PathValue("envSlug")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
