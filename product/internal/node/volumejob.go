package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Kinds of command a machine runs on one of its own volumes. The machine
// sends the archive straight to S3 or reads it straight from there, so a
// volume never crosses this channel.
const (
	CommandVolumeBackup  = "volume-backup"
	CommandVolumeRestore = "volume-restore"
)

// VolumeJobTimeout bounds one backup or restore of a volume on a machine.
const VolumeJobTimeout = 2 * time.Hour

// awayAfter is how long since a machine last called in before a volume job
// is refused rather than queued for a poll that may never come.
const awayAfter = 2 * time.Minute

// VolumeJob is a backup or restore of one volume on the machine it is on.
type VolumeJob struct {
	ID int64 `json:"id"`
	// App is the app's reference, which is its containers' LabelApp: they
	// are stopped for the copy.
	App string   `json:"app"`
	S3  S3Object `json:"s3"`
}

// S3Object is where the archive goes, with the login to put it there. The
// machine holds the login for the job and writes it nowhere.
type S3Object struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region,omitempty"`
	Secure    bool   `json:"secure"`
	PathStyle bool   `json:"path_style"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
}

// VolumeJobResult is a machine's answer to a volume job.
type VolumeJobResult struct {
	Size int64 `json:"size"`
}

// ErrMachineAway is a machine that has not called in recently.
var ErrMachineAway = errors.New("that server has not called in recently, so it cannot be asked to do this")

// RunVolumeJob has a machine back up or restore one of its volumes, and
// waits for it to finish — as long as ctx allows, since a copy is minutes.
// A machine too old to know the command answers that it does not.
func (s *Service) RunVolumeJob(ctx context.Context, nodeID int64, kind string, job VolumeJob) (int64, error) {
	quiet, err := s.Quiet(ctx, awayAfter)
	if err != nil {
		return 0, err
	}
	if quiet[nodeID] {
		return 0, ErrMachineAway
	}
	out, err := s.hub.ask(ctx, nodeID, Command{Kind: kind, Volume: &job}, 0)
	if err != nil {
		return 0, err
	}
	var res VolumeJobResult
	if err := json.Unmarshal(out, &res); err != nil {
		return 0, fmt.Errorf("read the server's answer: %w", err)
	}
	return res.Size, nil
}

// Ask sends one command to a machine and waits for what it says, for up
// to CommandTimeout.
func (h *hub) Ask(ctx context.Context, nodeID int64, cmd Command) ([]byte, error) {
	return h.ask(ctx, nodeID, cmd, CommandTimeout)
}

// expired is a timeout's channel, or nil — which never fires — for none.
func expired(d time.Duration) <-chan time.Time {
	if d <= 0 {
		return nil
	}
	return time.After(d)
}
