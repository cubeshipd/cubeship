package app

import (
	"errors"
	"fmt"
)

// HealthCheck is one HTTP endpoint required before an app's new container
// is considered ready. Checks run in order; every check must pass.
type HealthCheck struct {
	Path                string `json:"path"`
	Port                int    `json:"port"`
	InitialDelaySeconds int    `json:"initial_delay_seconds"`
	PeriodSeconds       int    `json:"period_seconds"`
	TimeoutSeconds      int    `json:"timeout_seconds"`
	FailureThreshold    int    `json:"failure_threshold"`
	SuccessThreshold    int    `json:"success_threshold"`
}

var ErrInvalidNetworking = errors.New("invalid app networking configuration")

func defaultHealthCheck(check HealthCheck) HealthCheck {
	if check.PeriodSeconds == 0 {
		check.PeriodSeconds = 10
	}
	if check.TimeoutSeconds == 0 {
		check.TimeoutSeconds = 5
	}
	if check.FailureThreshold == 0 {
		check.FailureThreshold = 3
	}
	if check.SuccessThreshold == 0 {
		check.SuccessThreshold = 1
	}
	return check
}

func validateNetworking(ports []int, checks []HealthCheck) ([]HealthCheck, error) {
	known := make(map[int]bool, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 || known[port] {
			return nil, fmt.Errorf("%w: internal ports must be unique numbers from 1 to 65535", ErrInvalidNetworking)
		}
		known[port] = true
	}
	if len(checks) > 20 {
		return nil, fmt.Errorf("%w: at most 20 health checks are allowed", ErrInvalidNetworking)
	}
	out := make([]HealthCheck, len(checks))
	for i, raw := range checks {
		check := defaultHealthCheck(raw)
		if check.Path == "" || !ValidHealthPath(check.Path) || check.Port < 1 || check.Port > 65535 || !known[check.Port] {
			return nil, fmt.Errorf("%w: health check %d needs a valid path and a configured internal port", ErrInvalidNetworking, i+1)
		}
		if check.InitialDelaySeconds < 0 || check.InitialDelaySeconds > 3600 ||
			check.PeriodSeconds < 1 || check.PeriodSeconds > 300 ||
			check.TimeoutSeconds < 1 || check.TimeoutSeconds > 60 ||
			check.FailureThreshold < 1 || check.FailureThreshold > 20 ||
			check.SuccessThreshold < 1 || check.SuccessThreshold > 20 {
			return nil, fmt.Errorf("%w: health check timing values are outside the allowed range", ErrInvalidNetworking)
		}
		if check.TimeoutSeconds >= check.PeriodSeconds {
			return nil, fmt.Errorf("%w: health check timeout must be shorter than its period", ErrInvalidNetworking)
		}
		out[i] = check
	}
	return out, nil
}
