package backup

import (
	"context"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tools are the MCP tools for backups. Only volumes' so far, and no
// restore: replacing data stays with a person.
type Tools struct {
	h      *Handler
	caller *user.User
}

// NewTools takes the Handler for how a backup is rendered, store names
// included, so the tools answer what the API does.
func NewTools(h *Handler, caller *user.User) *Tools {
	return &Tools{h: h, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_volume_backups",
		Description: "List the backups of one of an app's volumes, newest first: when each was taken, whether it succeeded, its size, and whether it left this machine. Get the volume's id from list_app_volumes. Requires the admin role.",
	}, t.listVolume)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "back_up_volume",
		Description: "Back up one of an app's volumes now. **The app is stopped while the copy is taken** and started again afterwards, whether or not it worked. The backup runs detached: this returns its row with status `taking`, and list_volume_backups says how it ended. It goes to an S3 store linked from outside this instance — the one named, or the volume's schedule's; this machine's disk and a store this instance runs are refused. A volume on another server is copied by that server, straight to the bucket. Requires the admin role.",
	}, t.takeVolume)
}

type volumeInput struct {
	Reference string `json:"reference" jsonschema:"the app's reference: project/environment/name"`
	VolumeID  int64  `json:"volume_id" jsonschema:"the volume's id, from list_app_volumes"`
}

type takeVolumeInput struct {
	Reference string `json:"reference" jsonschema:"the app's reference: project/environment/name"`
	VolumeID  int64  `json:"volume_id" jsonschema:"the volume's id, from list_app_volumes"`
	Store     string `json:"store,omitempty" jsonschema:"the S3 store linked from outside this instance to send it to, by name. Empty uses the volume's schedule"`
	Bucket    string `json:"bucket,omitempty" jsonschema:"the bucket in that store. Required with store"`
}

type backupList struct {
	Backups []Response `json:"backups"`
}

func (t *Tools) listVolume(ctx context.Context, _ *mcp.CallToolRequest, in volumeInput) (*mcp.CallToolResult, backupList, error) {
	rows, err := t.h.svc.ForVolume(ctx, t.caller, in.Reference, in.VolumeID)
	if err != nil {
		return nil, backupList{}, err
	}
	return nil, backupList{Backups: t.h.toResponses(rows)}, nil
}

func (t *Tools) takeVolume(ctx context.Context, _ *mcp.CallToolRequest, in takeVolumeInput) (*mcp.CallToolResult, Response, error) {
	row, err := t.h.svc.TakeVolume(ctx, t.caller, in.Reference, in.VolumeID, in.Store, in.Bucket)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, t.h.toResponse(row), nil
}
