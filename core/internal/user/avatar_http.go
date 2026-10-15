package user

import (
	"net/http"
	"strings"
)

func (h *Handler) avatar(w http.ResponseWriter, r *http.Request) {
	data, mediaType, version, err := h.svc.Avatar(r.Context(), FromContext(r.Context()), r.PathValue("username"))
	if err != nil {
		WriteError(w, err)
		return
	}
	etag := `"` + version + `"`
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	w.Header().Set("Vary", "Cookie, Authorization")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	_, _ = w.Write(data)
}

func (h *Handler) setAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.SetAvatar(r.Context(), FromContext(r.Context()), r.Body); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) clearAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ClearAvatar(r.Context(), FromContext(r.Context())); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
