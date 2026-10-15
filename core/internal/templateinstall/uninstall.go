package templateinstall

import (
	"context"
	"cubeship/internal/user"
)

// Uninstall deletes an installation's apps in the background — and its
// databases and stores too, unless keepData.
func (s *Service) Uninstall(ctx context.Context, caller *user.User, id int64, keepData bool) (*Run, error) {
	if err := user.Allow(caller, user.ResTemplates, RoleToInstall, ""); err != nil {
		return nil, err
	}
	in, err := s.records.Install(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Status != StatusInstalled {
		return nil, ErrNotInstalled
	}
	if busy, err := s.busy(ctx, id); err != nil {
		return nil, err
	} else if busy {
		return nil, ErrBusy
	}
	run, err := s.records.CreateRun(ctx, &Run{
		InstallID: id, Kind: RunUninstall, FromRelease: in.Release,
		Status: RunRunning, Step: "Starting", KeepData: keepData, CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, err
	}
	started := copyRun(run)
	s.start(func() { s.runUninstall(caller, in, run) })
	return started, nil
}
