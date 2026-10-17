package objectstore

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

func (h *Handler) credentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.Credentials(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		Region    string `json:"region"`
		Endpoint  string `json:"endpoint"`
		External  string `json:"external_endpoint,omitempty"`
		PathStyle bool   `json:"path_style"`
	}{creds.AccessKey, creds.SecretKey, creds.Region, creds.Endpoint, creds.External, creds.PathStyle})
}

func (h *Handler) rotateCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := h.svc.RotateCredentials(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		Region    string `json:"region"`
		Endpoint  string `json:"endpoint"`
		External  string `json:"external_endpoint,omitempty"`
		PathStyle bool   `json:"path_style"`
	}{creds.AccessKey, creds.SecretKey, creds.Region, creds.Endpoint, creds.External, creds.PathStyle})
}

// providers is what may be linked and what may be run, in one answer:
// a form that offers both reads one endpoint rather than two.
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if err := user.Require(user.FromContext(r.Context()), user.RoleMember); err != nil {
		WriteError(w, err)
		return
	}
	type provider struct {
		Provider string `json:"provider"`
		Label    string `json:"label"`
		Asks     string `json:"asks"`
		// ScopesByBucket says this provider's logins are commonly
		// issued for one bucket, so naming one is the expected answer
		// here and the unusual answer everywhere else. Every provider
		// accepts the field; this is what a form says about it.
		ScopesByBucket bool `json:"scopes_by_bucket"`
	}
	out := struct {
		Providers []provider `json:"providers"`
		Versions  []string   `json:"versions"`
	}{Versions: Versions()}
	for _, p := range Providers() {
		out.Providers = append(out.Providers,
			provider{string(p), p.Label(), string(p.Asks()), p.ScopesByBucket()})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rc, err := h.svc.Logs(ctx, user.FromContext(ctx), r.PathValue("name"), r.URL.Query().Get("tail"))
	if err != nil {
		WriteError(w, err)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Containers are created without a TTY, so the Engine returns
	// stdout and stderr multiplexed behind an 8-byte binary frame
	// header per chunk. Copying that straight through prints binary
	// garbage between the log lines — demultiplex it first.
	if _, err := stdcopy.StdCopy(w, w, rc); err != nil {
		// The status line is already sent; all we can do is record it.
		log.Printf("logs for object store %s: %v", r.PathValue("name"), err)
	}
}

// metrics is what a managed store's container has been using. The
// series is metrics' to render; what this adds is who may look at it,
// and that there is anything to look at.
//
// A linked store is refused rather than answered with an empty series.
// Nothing here samples somebody else's server, and "no samples" reads
// as a store that is idle rather than as one this instance was never
// measuring.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store, err := h.svc.Resolve(ctx, user.FromContext(ctx), r.PathValue("name"), user.LevelView)
	if err != nil {
		WriteError(w, err)
		return
	}
	if store.Kind != KindManaged {
		WriteError(w, ErrExternalHasNoContainer)
		return
	}
	metrics.WriteSeries(w, r, h.svc.Metrics(), metrics.KindObjectStore, store.ID, store.ContainerID != "")
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Start)
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Stop)
}

func (h *Handler) unexpose(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Unexpose)
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request,
	run func(context.Context, *user.User, string) (*Store, error),
) {
	ctx := r.Context()
	store, err := run(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(store, h.svc.ExternalHost(ctx)))
}

func (h *Handler) expose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	store, err := h.svc.Expose(ctx, user.FromContext(ctx), r.PathValue("name"), req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(store, h.svc.ExternalHost(ctx)))
}

func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App    string `json:"app"`
		Bucket string `json:"bucket"`
		Prefix string `json:"prefix"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	store, err := h.svc.Attach(ctx, user.FromContext(ctx),
		r.PathValue("name"), req.App, req.Bucket, req.Prefix)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(store, h.svc.ExternalHost(ctx)))
}

// detach takes the app's reference in the path, three segments of it,
// and the bucket in the query.
//
// The bucket cannot be a path segment beside them without reading as a
// fourth part of the app's name, and it has to be somewhere: unlike a
// datastore, one app may be attached to the same store twice — a bucket
// for uploads and one for backups — so "which attachment" needs both.
func (h *Handler) detach(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appRef := r.PathValue("project") + "/" + r.PathValue("env") + "/" + r.PathValue("app")
	store, err := h.svc.Detach(ctx, user.FromContext(ctx),
		r.PathValue("name"), appRef, r.URL.Query().Get("bucket"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(store, h.svc.ExternalHost(ctx)))
}

func (h *Handler) buckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	found, err := h.svc.Buckets(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]BucketResponse, 0, len(found))
	for _, b := range found {
		row := BucketResponse{Name: b.Name}
		if !b.CreatedAt.IsZero() {
			created := b.CreatedAt
			row.CreatedAt = &created
		}
		out = append(out, row)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	if err := h.svc.CreateBucket(ctx, user.FromContext(ctx), r.PathValue("name"), req.Name); err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, BucketResponse{Name: req.Name})
}

func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := h.svc.DeleteBucket(ctx, user.FromContext(ctx), r.PathValue("name"), r.PathValue("bucket"))
	if err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) browse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))

	listing, err := h.svc.Browse(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"),
		query.Get("prefix"), query.Get("cursor"), limit)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListingResponse(listing))
}

// toListingResponse renders one level of a bucket. Shared with the MCP
// surface, so the two cannot describe a folder differently.
func toListingResponse(listing Listing) ListingResponse {
	out := ListingResponse{
		Prefix:  listing.Prefix,
		Folders: make([]FolderResponse, 0, len(listing.Folders)),
		Objects: make([]ObjectResponse, 0, len(listing.Objects)),
		Cursor:  listing.Cursor,
	}
	for _, prefix := range listing.Folders {
		out.Folders = append(out.Folders, FolderResponse{Prefix: prefix, Name: FolderName(prefix)})
	}
	for _, o := range listing.Objects {
		out.Objects = append(out.Objects, ObjectResponse{
			Key: o.Key, Name: o.Name(), Size: o.Size, ModifiedAt: o.ModifiedAt, ETag: o.ETag,
		})
	}
	return out
}

// upload takes the file as the request body rather than as a form.
//
// Not multipart, and that is a security decision as much as a
// convenience one: multipart/form-data is one of the three content
// types a browser will send cross-site without a preflight, so an
// endpoint that accepted one would be reachable from any page the
// session's owner happens to open. A raw body forces a preflight the
// other origin cannot pass, on top of the same-origin check the session
// middleware already makes.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()

	// -1 when the sender did not say, which is a chunked upload. The
	// client library answers that by uploading in parts rather than
	// buffering the whole file to find out how big it is.
	size := r.ContentLength
	if size < 0 {
		size = -1
	}
	key, err := h.svc.Upload(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"),
		query.Get("prefix"), query.Get("filename"),
		r.Body, size, r.Header.Get("Content-Type"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ObjectResponse{Key: key, Name: Object{Key: key}.Name(), Size: size})
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := h.svc.DeleteObject(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prefix string `json:"prefix"`
		Name   string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	key, err := h.svc.CreateFolder(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"), req.Prefix, req.Name)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, FolderResponse{Prefix: key, Name: FolderName(key)})
}

func (h *Handler) deleteFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	removed, err := h.svc.DeleteFolder(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"), r.URL.Query().Get("prefix"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Removed int `json:"removed"`
	}{removed})
}

// download streams one object to whoever asked.
//
// Through the daemon rather than as a redirect to a presigned URL: a
// managed store's endpoint is a container name that resolves on the
// Docker network and nowhere else, so the link would be one only the
// daemon could follow. See Service.Download.
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := r.URL.Query().Get("key")
	body, info, err := h.svc.Download(ctx, user.FromContext(ctx),
		r.PathValue("name"), r.PathValue("bucket"), key)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer body.Close()

	contentType := info.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	// attachment, always. A bucket holds whatever the apps on this
	// instance put there, and rendering an uploaded HTML file inline
	// would run it on this daemon's own origin, beside the session
	// cookie. The filename is encoded both ways because the plain form
	// cannot carry a non-ASCII name and the starred one is what every
	// browser since IE has read.
	name := Object{Key: info.Key}.Name()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		strings.NewReplacer(`"`, "", "\\", "", "\r", "", "\n", "").Replace(name),
		url.PathEscape(name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, body); err != nil {
		// The status is long gone by now; there is nothing to tell the
		// client that it is not already finding out from a truncated
		// body.
		return
	}
}
