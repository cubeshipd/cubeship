package server

import (
	"context"

	"cubeship/internal/platform/hostexec"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/shell"
)

// terminals is what opens shells on this machine, or a stand-in that
// refuses for a server given none.
func terminals(local shell.Local) shell.Local {
	if local == nil {
		return unavailable{}
	}
	return local
}

type unavailable struct{}

func (unavailable) ContainerShell(context.Context, string, uint16, uint16) (terminal.Process, error) {
	return nil, hostexec.ErrUnavailable
}

func (unavailable) Terminal(context.Context, uint16, uint16) (terminal.Process, error) {
	return nil, hostexec.ErrUnavailable
}
