package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Backup is one backup, as the API lists it.
type Backup struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Volume     string `json:"volume"`
	Store      string `json:"store"`
	Bucket     string `json:"bucket"`
	OffMachine bool   `json:"off_machine"`
	Size       int64  `json:"size_bytes"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Scheduled  bool   `json:"scheduled"`
	StartedAt  string `json:"started_at"`
}

func volumeBackupsPath(ref string, volumeID int64) string {
	return fmt.Sprintf("%s/volumes/%d/backups", appPath(ref), volumeID)
}

func (c *Client) ListVolumeBackups(ctx context.Context, ref string, volumeID int64) ([]Backup, error) {
	return request[[]Backup](ctx, c, "list volume backups", http.MethodGet,
		volumeBackupsPath(ref, volumeID), nil, http.StatusOK, DefaultTimeout)
}

// TakeVolumeBackup starts one; it runs detached, so the row is `taking`.
func (c *Client) TakeVolumeBackup(ctx context.Context, ref string, volumeID int64, store, bucket string) (Backup, error) {
	var body any
	if store != "" {
		body = map[string]string{"store": store, "bucket": bucket}
	}
	return request[Backup](ctx, c, "back up volume", http.MethodPost,
		volumeBackupsPath(ref, volumeID), body, http.StatusAccepted, DefaultTimeout)
}

// RestoreBackup puts a backup back, replacing what is there.
// server, for a volume's backup, restores it on another server and moves
// the app there.
func (c *Client) RestoreBackup(ctx context.Context, id int64, server string) error {
	var body any
	if server != "" {
		body = map[string]string{"server": server}
	}
	_, err := request[noContent](ctx, c, "restore backup", http.MethodPost,
		fmt.Sprintf("/backups/%d/restore", id), body, http.StatusNoContent, 2*time.Hour)
	return err
}
