package server

import (
	"context"
	"errors"
	"fmt"

	"cubeship/internal/app"
	"cubeship/internal/backup"
	"cubeship/internal/user"
)

// appVolumes is how `backup` reaches an app's volumes: the lookup, and
// stopping the app for the copy.
type appVolumes struct{ apps *app.Service }

func (a appVolumes) VolumeOf(ctx context.Context, caller *user.User, ref string, id int64) (*backup.Volume, error) {
	r, err := app.ParseReference(ref)
	if err != nil {
		return nil, backup.ErrNotFound
	}
	return volumeFor(a.apps.VolumeOf(ctx, caller, r, id))
}

func (a appVolumes) VolumeByID(ctx context.Context, id int64) (*backup.Volume, error) {
	return volumeFor(a.apps.VolumeByID(ctx, id))
}

func (a appVolumes) AllVolumes(ctx context.Context) ([]*backup.Volume, error) {
	all, err := a.apps.AllVolumes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*backup.Volume, 0, len(all))
	for _, t := range all {
		v, _ := volumeFor(t, nil)
		out = append(out, v)
	}
	return out, nil
}

func (a appVolumes) WithAppStopped(ctx context.Context, appID int64, fn func() error) error {
	return a.apps.WithAppStopped(ctx, appID, fn)
}

func volumeFor(t *app.VolumeTarget, err error) (*backup.Volume, error) {
	if errors.Is(err, app.ErrVolumeNotFound) || errors.Is(err, app.ErrNotFound) {
		return nil, backup.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &backup.Volume{
		ID: t.ID, AppID: t.AppID, NodeID: t.NodeID, App: t.App.String(), Path: t.Path,
		Dir: t.Dir, OnWorker: t.OnWorker,
	}, nil
}

func (a appVolumes) ServerFor(ctx context.Context, server string) (int64, bool, error) {
	id, controlPlane, err := a.apps.ServerFor(ctx, server)
	if errors.Is(err, app.ErrNoSuchNode) {
		return 0, false, backup.ErrNoSuchServer
	}
	return id, controlPlane, err
}

func (a appVolumes) MoveVolume(ctx context.Context, volumeID int64, server string) error {
	err := a.apps.MoveVolume(ctx, volumeID, server)
	switch {
	case errors.Is(err, app.ErrVolumeMoveOne):
		return backup.ErrVolumeMoveOne
	case errors.Is(err, app.ErrNoSuchNode):
		return backup.ErrNoSuchServer
	case errors.Is(err, app.ErrNotPlaceable):
		return fmt.Errorf("%w: %v", backup.ErrCannotMove, err)
	}
	return err
}
