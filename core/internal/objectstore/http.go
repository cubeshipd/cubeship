package objectstore

import (
	"errors"
	"net/http"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Response is one store as the API returns it.
//
// There are no keys on it. A store's login exists to be sent to an
// endpoint, and a listing that carried it would turn every read of this
// screen into a way out for it — see the credentials endpoint, which is
// its own request and an admin's.
type Response struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider"`
	// ProviderLabel is the provider's name as a person writes it, so
	// every surface spells "DigitalOcean Spaces" the same way.
	ProviderLabel string `json:"provider_label"`

	// Endpoint is where this answers, as a client dials it.
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	PathStyle bool   `json:"path_style"`
	// ScopesByBucket says this store's provider issues logins for a
	// single bucket, which is what decides how a form phrases the
	// optional bucket field rather than whether it offers one. Served
	// rather than derived in the dashboard for the reason ProviderLabel
	// is: a second list of which providers those are is a list that
	// disagrees with the daemon the first time one is added.
	ScopesByBucket bool `json:"scopes_by_bucket"`
	// Bucket is the one this store is pinned to, when it is. Absent for
	// a store that lists its own.
	Bucket string `json:"bucket,omitempty"`
	// CredentialID is the stored account an external store
	// authenticates as. Absent on a managed one, whose keys are its
	// own.
	CredentialID int64 `json:"credential_id,omitempty"`

	Version string `json:"version,omitempty"`
	// ExposedPort is the host port a managed store also answers on.
	// Absent when it does not, which is the default.
	ExposedPort int `json:"exposed_port,omitempty"`
	// ExternalEndpoint is where something off this host reaches it.
	// Present only while it is exposed and the instance has a domain.
	ExternalEndpoint string `json:"external_endpoint,omitempty"`
	// Limits is how much of the machine a managed store's container may
	// take. Zero in either half is no limit, which is the default, and
	// both are always zero on a linked store — there is no container
	// here to cap.
	Limits Limits `json:"limits"`

	// HasContainer says whether a container currently backs this, which
	// is what decides whether there is a log to read or anything to
	// stop. The status alone cannot answer it: one whose provisioning
	// failed may have neither.
	HasContainer bool   `json:"has_container"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`

	// Attachments are the apps this store's variables reach.
	Attachments []AttachmentResponse `json:"attachments"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AttachmentResponse is one app wired to one bucket.
//
// It carries the variable *names* and not their values: one of them is
// the secret key, and a screen that says what an app receives does not
// have to say what is in it.
type AttachmentResponse struct {
	App       string   `json:"app"`
	Bucket    string   `json:"bucket"`
	Prefix    string   `json:"prefix,omitempty"`
	Variables []string `json:"variables"`
}

func toResponse(s *Store, domain string) Response {
	return Response{
		Name: s.Slug, Description: s.Description,
		Kind: string(s.Kind), Provider: string(s.Provider), ProviderLabel: s.Provider.Label(),
		Endpoint: s.URL(), Region: s.Region, PathStyle: s.PathStyle,
		Bucket: s.Bucket, ScopesByBucket: s.Provider.ScopesByBucket(),
		CredentialID: s.CredentialID, Version: s.Version, ExposedPort: s.ExposedPort,
		Limits:           s.Limits,
		ExternalEndpoint: s.ExternalURL(domain),
		HasContainer:     s.ContainerID != "", Status: s.Status, Error: s.Error,
		Attachments: toAttachments(s.Attachments),
		CreatedAt:   s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func toAttachments(all []Attachment) []AttachmentResponse {
	out := make([]AttachmentResponse, 0, len(all))
	for _, a := range all {
		out = append(out, AttachmentResponse{
			App: a.AppRef, Bucket: a.Bucket, Prefix: a.Prefix,
			Variables: VarNames(a.Prefix),
		})
	}
	return out
}

// BucketResponse is one bucket in a listing.
type BucketResponse struct {
	Name string `json:"name"`
	// CreatedAt is absent for a store pinned to one bucket, which is
	// reported without asking the endpoint anything.
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// ObjectResponse is one file.
type ObjectResponse struct {
	// Key is the whole key from the root of the bucket, and Name is its
	// last segment. Both, because one is what every other call takes
	// and the other is what a screen shows.
	Key        string    `json:"key"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	ETag       string    `json:"etag,omitempty"`
}

// FolderResponse is one folder — a common prefix, which is the only
// kind of folder S3 has.
type FolderResponse struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

// ListingResponse is one level of one bucket.
type ListingResponse struct {
	Prefix  string           `json:"prefix"`
	Folders []FolderResponse `json:"folders"`
	Objects []ObjectResponse `json:"objects"`
	// Cursor continues the listing. Absent when there is no more.
	Cursor string `json:"cursor,omitempty"`
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /objectstores", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /objectstores", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /objectstores/providers", auth(http.HandlerFunc(h.providers)))
	r.Handle("GET /objectstores/{name}", auth(http.HandlerFunc(h.get)))
	r.Handle("PATCH /objectstores/{name}", auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE /objectstores/{name}", auth(http.HandlerFunc(h.delete)))
	r.Handle("GET /objectstores/{name}/credentials", auth(http.HandlerFunc(h.credentials)))
	r.Handle("POST /objectstores/{name}/credentials/rotate", auth(http.HandlerFunc(h.rotateCredentials)))
	r.Handle("GET /objectstores/{name}/logs", auth(http.HandlerFunc(h.logs)))
	r.Handle("GET /objectstores/{name}/metrics", auth(http.HandlerFunc(h.metrics)))
	r.Handle("POST /objectstores/{name}/start", auth(http.HandlerFunc(h.start)))
	r.Handle("POST /objectstores/{name}/stop", auth(http.HandlerFunc(h.stop)))
	r.Handle("POST /objectstores/{name}/expose", auth(http.HandlerFunc(h.expose)))
	r.Handle("DELETE /objectstores/{name}/expose", auth(http.HandlerFunc(h.unexpose)))

	r.Handle("POST /objectstores/{name}/attachments", auth(http.HandlerFunc(h.attach)))
	r.Handle("DELETE /objectstores/{name}/attachments/{project}/{env}/{app}",
		auth(http.HandlerFunc(h.detach)))

	r.Handle("GET /objectstores/{name}/buckets", auth(http.HandlerFunc(h.buckets)))
	r.Handle("POST /objectstores/{name}/buckets", auth(http.HandlerFunc(h.createBucket)))
	r.Handle("DELETE /objectstores/{name}/buckets/{bucket}", auth(http.HandlerFunc(h.deleteBucket)))
	r.Handle("GET /objectstores/{name}/buckets/{bucket}/objects", auth(http.HandlerFunc(h.browse)))
	r.Handle("PUT /objectstores/{name}/buckets/{bucket}/objects", auth(http.HandlerFunc(h.upload)))
	r.Handle("DELETE /objectstores/{name}/buckets/{bucket}/objects", auth(http.HandlerFunc(h.deleteObject)))
	r.Handle("GET /objectstores/{name}/buckets/{bucket}/download", auth(http.HandlerFunc(h.download)))
	r.Handle("POST /objectstores/{name}/buckets/{bucket}/folders", auth(http.HandlerFunc(h.createFolder)))
	r.Handle("DELETE /objectstores/{name}/buckets/{bucket}/folders", auth(http.HandlerFunc(h.deleteFolder)))
}

// WriteError maps this module's domain errors onto status codes.
//
// The two families are kept apart on purpose. This instance's own
// refusals are 400/403/404/409; what the *endpoint* said is 404 for a
// missing bucket, 403 for a login it rejected, and 502 for one that did
// not answer — because a store this daemon cannot reach is an upstream
// failure, not a bad request, and reading it as one sends somebody
// looking at what they typed.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)

	case errors.Is(err, ErrNotFound), errors.Is(err, database.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrBucketNotFound), errors.Is(err, ErrObjectNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrPortTaken),
		errors.Is(err, ErrBucketNotEmpty), errors.Is(err, ErrAlreadyAttached),
		errors.Is(err, ErrPrefixTaken):
		http.Error(w, err.Error(), http.StatusConflict)

	case errors.Is(err, ErrNotAttached):
		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, ErrDenied):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrUnreachable):
		http.Error(w, err.Error(), http.StatusBadGateway)

	case errors.Is(err, ErrNotRunning), errors.Is(err, ErrExternalHasNoContainer),
		errors.Is(err, ErrManagedFixed), errors.Is(err, ErrLinkedHasNoContainer),
		// Nothing about the request is malformed: something else on the
		// instance stands in the way, and the answer names it.
		errors.Is(err, ErrAttachedElsewhere):
		http.Error(w, err.Error(), http.StatusConflict)

	case errors.Is(err, slug.ErrInvalid), errors.Is(err, slug.ErrReserved),
		errors.Is(err, ErrReservedSlug), errors.Is(err, ErrUnknownKind),
		errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrUnknownVersion),
		errors.Is(err, ErrTwoLogins), errors.Is(err, ErrCredentialRequired),
		errors.Is(err, ErrRegionRequired), errors.Is(err, ErrAccountRequired),
		errors.Is(err, ErrInvalidRegion), errors.Is(err, ErrInvalidAccount),
		errors.Is(err, ErrEndpointRequired), errors.Is(err, ErrBadEndpoint),
		errors.Is(err, ErrBadBucket), errors.Is(err, ErrBadKey),
		errors.Is(err, ErrInvalidLimits),
		errors.Is(err, ErrSingleBucket),
		errors.Is(err, ErrBadPrefix),
		errors.Is(err, ErrBadPort),
		errors.Is(err, ErrNoPortsLeft), errors.Is(err, httpx.ErrNotJSON):
		http.Error(w, err.Error(), http.StatusBadRequest)

	default:
		// A login typed here is stored as a credential, so its refusals
		// — an empty label, one already taken — are that module's to
		// phrase rather than this one guessing at a status for an error
		// it did not raise.
		credential.WriteError(w, err)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stores, err := h.svc.List(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	// The domain is read once for the whole listing rather than per
	// row: it is the same answer for all of them.
	domain := h.svc.ExternalHost(ctx)
	out := make([]Response, 0, len(stores))
	for _, s := range stores {
		out = append(out, toResponse(s, domain))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store, err := h.svc.Resolve(ctx, user.FromContext(ctx), r.PathValue("name"), user.LevelView)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(store, h.svc.ExternalHost(ctx)))
}

// create is both kinds, discriminated by `kind`.
//
// One endpoint, because a store is one resource. Two would make "run
// one here" and "point at one there" look like different things to
// create, when what comes back is the same row and every screen after
// this treats them alike.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Description string `json:"description"`

		// Managed.
		Version   string `json:"version"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		Expose    *int   `json:"expose"`

		// External.
		Provider     string `json:"provider"`
		Region       string `json:"region"`
		Account      string `json:"account"`
		Endpoint     string `json:"endpoint"`
		Bucket       string `json:"bucket"`
		CredentialID int64  `json:"credential_id"`
		// Or keys typed here, for somebody with no stored account yet.
		Label     string `json:"label"`
		NewKey    string `json:"new_access_key"`
		NewSecret string `json:"new_secret_key"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	caller := user.FromContext(ctx)

	var (
		store *Store
		err   error
	)
	switch Kind(req.Kind) {
	case KindManaged:
		store, err = h.svc.Create(ctx, caller, ManagedSpec{
			Slug: req.Name, Description: req.Description, Version: req.Version,
			AccessKey: req.AccessKey, SecretKey: req.SecretKey, Expose: req.Expose,
		})
	case KindExternal:
		var login *NewLogin
		if req.NewKey != "" || req.NewSecret != "" {
			login = &NewLogin{Label: req.Label, AccessKey: req.NewKey, SecretKey: req.NewSecret}
		}
		store, err = h.svc.Link(ctx, caller, LinkSpec{
			Slug: req.Name, Description: req.Description, Provider: Provider(req.Provider),
			Region: req.Region, Account: req.Account, Endpoint: req.Endpoint,
			Bucket: req.Bucket, CredentialID: req.CredentialID,
		}, login)
	default:
		WriteError(w, ErrUnknownKind)
		return
	}
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(store, h.svc.ExternalHost(ctx)))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description  *string `json:"description"`
		CredentialID *int64  `json:"credential_id"`
		// Bucket moves the pin, and empty is how a store is unpinned —
		// a value rather than a gap, so leaving the field out is the
		// only way of saying "as it is". Unpinning is accepted on any
		// provider; pinning follows the same rule linking does.
		Bucket *string `json:"bucket"`
		// Limits is how much of the machine a managed store's
		// container may take. Sent as an object so that clearing a
		// ceiling is a value rather than a gap, and refused outright
		// on a linked store — there is no container here to cap.
		Limits *Limits `json:"limits"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	store, err := h.svc.Update(ctx, user.FromContext(ctx), r.PathValue("name"),
		req.Description, req.CredentialID, req.Bucket, req.Limits)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(store, h.svc.ExternalHost(ctx)))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := h.svc.Delete(ctx, user.FromContext(ctx), r.PathValue("name")); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
