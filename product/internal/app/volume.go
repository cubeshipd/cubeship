package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Volume is a directory on one machine's disk, mounted into an app's
// container at Path, that outlives the container.
//
// **It pins the app.** The data is on one machine and does not move, and
// two containers writing one directory is how RabbitMQ and Elasticsearch
// corrupt themselves — so an app with a volume runs as one copy on the
// machine the data is on, and its deploys stop the old container before
// the new one starts. See Orchestrator.swapInPlace.
type Volume struct {
	ID    int64
	AppID int64
	// Path is where the container sees it: absolute and cleaned.
	Path string
	// NodeID is the machine the data is on, and NodeSlug its name.
	NodeID    int64
	NodeSlug  string
	CreatedAt time.Time
}

var (
	ErrInvalidVolumePath = errors.New("a volume path is an absolute path inside the container: not /, not under /proc, /sys or /dev, and without a colon or a comma")
	ErrVolumeExists      = errors.New("this app already has a volume at that path")
	ErrVolumeNotFound    = errors.New("no such volume on this app")
	// ErrVolumeNeedsOneCopy is adding a volume to an app that runs as more
	// than one copy, on more than one machine, or chooses its own count.
	ErrVolumeNeedsOneCopy = errors.New("a volume needs the app to run as one copy on one machine, with spread and autoscaling off")
	// ErrVolumePinsApp is the other direction: changing where an app
	// with a volume runs, or how many of it.
	ErrVolumePinsApp = errors.New("this app has a volume, so it runs as one copy on the machine its data is on; data does not move between machines")
	// ErrVolumeOnWorker is deleting data this daemon cannot reach.
	ErrVolumeOnWorker = errors.New("that volume's data is on another machine, which this instance cannot delete files on; remove the directory on that machine")
	ErrNoDataDir      = errors.New("this daemon has no data directory to keep volumes in")
	ErrOrphanNotFound = errors.New("no kept volume data with that id")
)

// reservedMounts are the paths a container gets from the kernel, which a
// bind over would break.
var reservedMounts = []string{"/proc", "/sys", "/dev"}

// CleanVolumePath is a path somebody typed, as the container will see it,
// or ErrInvalidVolumePath.
//
// A colon and a comma are refused because a bind is written
// `host:container:options`: one in the path would be read as the start of
// the options.
func CleanVolumePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, ":,\x00") {
		return "", ErrInvalidVolumePath
	}
	clean := path.Clean(p)
	if clean == "/" {
		return "", ErrInvalidVolumePath
	}
	for _, reserved := range reservedMounts {
		if clean == reserved || strings.HasPrefix(clean, reserved+"/") {
			return "", ErrInvalidVolumePath
		}
	}
	return clean, nil
}

// VolumeDir is where a volume's data is, on the machine it is on. Keyed by
// id for the reason a datastore's is: the id is the one thing about it
// that is not a name.
func VolumeDir(dataDir string, id int64) string {
	return filepath.Join(dataDir, "volumes", strconv.FormatInt(id, 10))
}

// VolumeBind is the Engine's bind for one volume.
func VolumeBind(dataDir string, id int64, containerPath string) string {
	return VolumeDir(dataDir, id) + ":" + containerPath
}

// volumeRecord is what is written beside a volume's data, so kept data
// can still say what it was once its row is gone. Beside rather than
// inside: inside, the container would see it.
type volumeRecord struct {
	App       string    `json:"app"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

func volumeRecordPath(dataDir string, id int64) string {
	return VolumeDir(dataDir, id) + ".json"
}

// prepareVolume makes a volume's directory and writes its record.
func prepareVolume(dataDir, app string, v *Volume) error {
	if dataDir == "" {
		return ErrNoDataDir
	}
	if err := os.MkdirAll(VolumeDir(dataDir, v.ID), 0o755); err != nil {
		return fmt.Errorf("make the volume's directory: %w", err)
	}
	record, err := json.Marshal(volumeRecord{App: app, Path: v.Path, CreatedAt: v.CreatedAt})
	if err != nil {
		return err
	}
	if err := os.WriteFile(volumeRecordPath(dataDir, v.ID), record, 0o600); err != nil {
		return fmt.Errorf("write the volume's record: %w", err)
	}
	return nil
}

// removeVolumeData deletes a volume's directory and its record.
func removeVolumeData(dataDir string, id int64) error {
	if dataDir == "" {
		return ErrNoDataDir
	}
	if err := os.RemoveAll(VolumeDir(dataDir, id)); err != nil {
		return fmt.Errorf("delete the volume's data: %w", err)
	}
	if err := os.Remove(volumeRecordPath(dataDir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete the volume's record: %w", err)
	}
	return nil
}

// Orphan is a volume's data kept after its volume was removed.
type Orphan struct {
	ID int64
	// App and Path are what it was, from its record. Empty when the
	// record is missing.
	App       string
	Path      string
	CreatedAt time.Time
}

// orphans is every volume directory on this machine that no volume row
// names, newest id first.
func orphans(dataDir string, known map[int64]bool) ([]Orphan, error) {
	if dataDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "volumes"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the volumes directory: %w", err)
	}
	var out []Orphan
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || id <= 0 || known[id] {
			continue
		}
		o := Orphan{ID: id}
		if raw, err := os.ReadFile(volumeRecordPath(dataDir, id)); err == nil {
			var r volumeRecord
			if json.Unmarshal(raw, &r) == nil {
				o.App, o.Path, o.CreatedAt = r.App, r.Path, r.CreatedAt
			}
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// keepsVolume reports whether a placement leaves an app with a volume
// where its data is, as one copy.
func keepsVolume(a *Scoped, p Placement) bool {
	if p.Spread != nil && *p.Spread {
		return false
	}
	if p.Replicas > 1 {
		return false
	}
	nodes := dedupe(p.Nodes)
	if len(nodes) == 0 {
		return true
	}
	return len(nodes) == 1 && nodes[0] == a.Volumes[0].NodeSlug
}

// canHoldVolume reports whether an app is in the shape a volume needs.
func canHoldVolume(a *App) bool {
	return len(a.Replicas) == 1 && !a.Spread && !a.Autoscale.On() && a.Scale <= 1
}
