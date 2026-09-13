package server

import (
	"context"
	"errors"

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
		ID: t.ID, AppID: t.AppID, App: t.App.String(), Path: t.Path,
		Dir: t.Dir, OnWorker: t.OnWorker,
	}, nil
}
