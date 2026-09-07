package objectstore

import (
	"context"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools describe the storage and never its contents, and they
// change nothing.
//
// **No tool reads or writes an object.** A bucket holds whatever the
// apps on this instance put there — uploads, database dumps, a config
// file with a key in it — and a tool that downloads one would put that
// through a model's context, which is the same objection
// internal/extregistry makes to having any tools at all. Listing what
// is *there* is a different act from reading what is *in* it, and the
// line is drawn between them: an agent can see that
// `backups/2026-09-01.sql.gz` exists and is 400 MB, and cannot open it.
//
// **No tool creates, links, exposes or deletes a store.** Creating one
// starts a container and claims disk; linking one means an access key
// passing through a model's context; exposing one puts an endpoint on
// the open internet. Each of those is an operator's decision made
// deliberately on a screen, and none of them is worth automating.
type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_object_stores",
		Description: "List the object storage this instance can reach: the MinIO servers it runs and the S3 endpoints it holds keys for. Each carries the endpoint an app on this instance connects to, so this is how you find out where an app should write. Keys are never reported.",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_object_store",
		Description: "Get one object store by name: where it answers, which provider it is, and — for one this instance runs — whether it is up. Keys are never reported.",
	}, t.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_buckets",
		Description: "List the buckets in one object store. Requires the admin role, like everything about a store's contents.",
	}, t.buckets)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_objects",
		Description: "List one level of one bucket: the folders directly under a prefix and the files directly in it, with their sizes and when each was last written. Names and sizes only — no tool here reads what is in a file. Requires the admin role.",
	}, t.objects)
}

type nameInput struct {
	Store string `json:"store" jsonschema:"the object store's name on this instance"`
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []Response, error) {
	all, err := t.svc.List(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	domain := t.svc.ExternalHost(ctx)
	out := make([]Response, 0, len(all))
	for _, s := range all {
		out = append(out, toResponse(s, domain))
	}
	return nil, out, nil
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, Response, error) {
	store, err := t.svc.Resolve(ctx, t.caller, in.Store, user.RoleMember)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(store, t.svc.ExternalHost(ctx)), nil
}

func (t *Tools) buckets(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, []BucketResponse, error) {
	found, err := t.svc.Buckets(ctx, t.caller, in.Store)
	if err != nil {
		return nil, nil, err
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
	return nil, out, nil
}

type objectsInput struct {
	Store  string `json:"store" jsonschema:"the object store's name on this instance"`
	Bucket string `json:"bucket" jsonschema:"the bucket to look in"`
	Prefix string `json:"prefix,omitempty" jsonschema:"the folder to list, e.g. \"backups/2026/\". Empty for the root of the bucket"`
	Cursor string `json:"cursor,omitempty" jsonschema:"continue a listing that reported one. Omit for the first page"`
}

func (t *Tools) objects(ctx context.Context, _ *mcp.CallToolRequest, in objectsInput) (*mcp.CallToolResult, ListingResponse, error) {
	listing, err := t.svc.Browse(ctx, t.caller, in.Store, in.Bucket, in.Prefix, in.Cursor, 0)
	if err != nil {
		return nil, ListingResponse{}, err
	}
	return nil, toListingResponse(listing), nil
}
