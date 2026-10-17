package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// AuditEvent is one change, or one refused attempt, on the instance.
type AuditEvent struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Username string    `json:"username"`
	Via      string    `json:"via"`
	KeyName  string    `json:"key_name,omitempty"`
	Summary  string    `json:"summary"`
	Action   string    `json:"action"`
	Target   string    `json:"target,omitempty"`
	Outcome  string    `json:"outcome"`
	Status   int       `json:"status,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	IP       string    `json:"ip,omitempty"`
}

type AuditPage struct {
	Events []AuditEvent `json:"events"`
	Next   int64        `json:"next,omitempty"`
}

// AuditFilter narrows ListAudit; zero values filter nothing.
type AuditFilter struct {
	User, Via, Outcome, Target string
	// From and To are RFC 3339 or YYYY-MM-DD, as the daemon reads them.
	From, To string
	Before   int64
	Limit    int
}

func (c *Client) ListAudit(ctx context.Context, f AuditFilter) (AuditPage, error) {
	q := url.Values{}
	for k, v := range map[string]string{"user": f.User, "via": f.Via, "outcome": f.Outcome, "target": f.Target, "from": f.From, "to": f.To} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.Before > 0 {
		q.Set("before", strconv.FormatInt(f.Before, 10))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	path := "/audit"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return request[AuditPage](ctx, c, "read the audit log", http.MethodGet, path, nil, http.StatusOK, DefaultTimeout)
}
