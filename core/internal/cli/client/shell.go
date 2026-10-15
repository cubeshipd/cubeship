package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/terminal"

	"github.com/coder/websocket"
)

// AppShell connects to a shell in an app's container. server picks the
// machine when the app runs on several; empty takes the first running
// copy.
func (c *Client) AppShell(ctx context.Context, ref, server string, size terminal.Size) (*websocket.Conn, error) {
	q := sizeQuery(size)
	if server != "" {
		q.Set("server", server)
	}
	return c.dialShell(ctx, "open a shell", appPath(ref)+"/shell?"+q.Encode())
}

// ServerShell connects to a root shell on a machine.
func (c *Client) ServerShell(ctx context.Context, name string, size terminal.Size) (*websocket.Conn, error) {
	return c.dialShell(ctx, "open a shell", "/nodes/"+segment(name)+"/shell?"+sizeQuery(size).Encode())
}

func (c *Client) dialShell(ctx context.Context, op, path string) (*websocket.Conn, error) {
	base := c.baseURL
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	conn, resp, err := websocket.Dial(ctx, base+httpx.APIPrefix+path, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}},
	})
	if err != nil {
		// A refusal before the upgrade is the daemon's own answer —
		// signed out, a missing route on an older daemon — and it is
		// worth saying in its words rather than the library's.
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%s: this instance does not have shells — it may be older than this CLI", op)
			}
			return nil, apiError(op, resp)
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return conn, nil
}

func sizeQuery(s terminal.Size) url.Values {
	q := url.Values{}
	if s.Valid() {
		q.Set("cols", strconv.Itoa(int(s.Cols)))
		q.Set("rows", strconv.Itoa(int(s.Rows)))
	}
	return q
}
