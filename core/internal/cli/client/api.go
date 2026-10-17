package client

import (
	"errors"
)

// errorsAs is errors.As, wrapped so client.go needn't import errors just
// for Status.
func errorsAs(err error, target any) bool { return errors.As(err, target) }

// On reports whether the instance is scaling this app.
func (a Autoscale) On() bool { return a.Max > 0 }

// noContent is the Out type for an endpoint that answers with an empty
// body.
type noContent = struct{}

type envVars struct {
	Vars map[string]string `json:"vars"`
}
