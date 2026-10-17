package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"cubeship/internal/platform/database"
)

// ControlPlaneID is the machine this daemon is, by id.
//
// Read rather than configured: the row is seeded by the migration that
// created the table, and the one thing this module needs it for is
// telling its own replicas from the ones somebody else runs. Reading
// the `nodes` table directly is what the scoped query already does.
func (r *Repository) ControlPlaneID(ctx context.Context) (int64, error) {
	var id int64
	if err := r.q.QueryRowContext(ctx,
		`SELECT id FROM nodes WHERE control_plane`).Scan(&id); err != nil {
		return 0, fmt.Errorf("find the control plane: %w", err)
	}
	return id, nil
}

// ListScoped returns every app on the instance with its containing
// slugs — what an admin sees.
func (r *Repository) ListScoped(ctx context.Context) ([]*Scoped, error) {
	return r.listScoped(ctx, scopedQuery+` ORDER BY a.id`)
}

func (r *Repository) listScoped(ctx context.Context, query string, args ...any) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Scoped
	for rows.Next() {
		s, err := scanScoped(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps := make([]*App, 0, len(out))
	for _, s := range out {
		apps = append(apps, &s.App)
	}
	return out, r.attach(ctx, apps)
}

// Domains reads every name an app is served at.
//
// A separate read rather than a join: an app's row is fetched in
// listings where the domains are not looked at, and a join would turn
// one row per app into one per domain for every one of them.
func (r *Repository) Domains(ctx context.Context, appID int64) ([]Domain, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, host, port FROM app_domains WHERE app_id = $1 ORDER BY id`, appID)
	if err != nil {
		return nil, fmt.Errorf("list app domains: %w", err)
	}
	defer rows.Close()

	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Host, &d.Port); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DomainsFor reads the domains of several apps at once, keyed by app.
//
// A listing shows what each app answers at, and asking per app would be
// one query per row.
func (r *Repository) DomainsFor(ctx context.Context, appIDs []int64) (map[int64][]Domain, error) {
	out := map[int64][]Domain{}
	if len(appIDs) == 0 {
		return out, nil
	}
	// Placeholders rather than ANY($1): passing a slice through
	// database/sql depends on the driver marshalling it, and the rest of
	// this package never asks that of it.
	holders := make([]string, len(appIDs))
	args := make([]any, len(appIDs))
	for i, id := range appIDs {
		holders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT app_id, id, host, port FROM app_domains
		 WHERE app_id IN (`+strings.Join(holders, ", ")+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list app domains: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var appID int64
		var d Domain
		if err := rows.Scan(&appID, &d.ID, &d.Host, &d.Port); err != nil {
			return nil, err
		}
		out[appID] = append(out[appID], d)
	}
	return out, rows.Err()
}

// AddDomain gives an app a name to answer at.
// HostTaken reports whether any app on the instance answers at host.
func (r *Repository) HostTaken(ctx context.Context, host string) (bool, error) {
	var taken bool
	err := r.q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM app_domains WHERE host = $1)`, host).Scan(&taken)
	return taken, err
}

func (r *Repository) AddDomain(ctx context.Context, appID int64, host string, port int) (*Domain, error) {
	var d Domain
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO app_domains (app_id, host, port) VALUES ($1, $2, $3) RETURNING id, host, port`,
		appID, host, port).Scan(&d.ID, &d.Host, &d.Port)
	if err != nil {
		return nil, fmt.Errorf("add app domain: %w", err)
	}
	return &d, nil
}

// SetDomainPort changes what one name reaches.
func (r *Repository) SetDomainPort(ctx context.Context, appID, domainID int64, port int) error {
	result, err := r.q.ExecContext(ctx,
		`UPDATE app_domains SET port = $1 WHERE id = $2 AND app_id = $3`, port, domainID, appID)
	if err != nil {
		return fmt.Errorf("set app domain port: %w", err)
	}
	return affected(result)
}

// RemoveDomain takes a name off an app.
func (r *Repository) RemoveDomain(ctx context.Context, appID, domainID int64) error {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM app_domains WHERE id = $1 AND app_id = $2`, domainID, appID)
	if err != nil {
		return fmt.Errorf("remove app domain: %w", err)
	}
	return affected(result)
}

// VolumesFor reads the volumes of several apps at once, keyed by app.
func (r *Repository) VolumesFor(ctx context.Context, appIDs []int64) (map[int64][]Volume, error) {
	out := map[int64][]Volume{}
	if len(appIDs) == 0 {
		return out, nil
	}
	rows, err := r.q.QueryContext(ctx, `
		SELECT v.id, v.app_id, v.path, v.node_id, n.slug, v.created_at
		FROM app_volumes v
		JOIN nodes n ON n.id = v.node_id
		WHERE v.app_id = ANY($1)
		ORDER BY v.app_id, v.path`, appIDs)
	if err != nil {
		return nil, fmt.Errorf("list app volumes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v Volume
		if err := rows.Scan(&v.ID, &v.AppID, &v.Path, &v.NodeID, &v.NodeSlug, &v.CreatedAt); err != nil {
			return nil, err
		}
		out[v.AppID] = append(out[v.AppID], v)
	}
	return out, rows.Err()
}

// VolumeByID is one volume, whatever app it belongs to.
func (r *Repository) VolumeByID(ctx context.Context, id int64) (*Volume, error) {
	var v Volume
	err := r.q.QueryRowContext(ctx, `
		SELECT v.id, v.app_id, v.path, v.node_id, n.slug, v.created_at
		FROM app_volumes v JOIN nodes n ON n.id = v.node_id
		WHERE v.id = $1`, id).Scan(&v.ID, &v.AppID, &v.Path, &v.NodeID, &v.NodeSlug, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVolumeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get app volume: %w", err)
	}
	return &v, nil
}

// AddVolume records a volume on the machine its data will be on.
func (r *Repository) AddVolume(ctx context.Context, appID int64, containerPath string, nodeID int64) (*Volume, error) {
	var v Volume
	err := r.q.QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO app_volumes (app_id, path, node_id) VALUES ($1, $2, $3)
			RETURNING id, app_id, path, node_id, created_at
		)
		SELECT i.id, i.app_id, i.path, i.node_id, n.slug, i.created_at
		FROM inserted i JOIN nodes n ON n.id = i.node_id`,
		appID, containerPath, nodeID).Scan(&v.ID, &v.AppID, &v.Path, &v.NodeID, &v.NodeSlug, &v.CreatedAt)
	if database.IsUniqueViolation(err) {
		return nil, ErrVolumeExists
	}
	if err != nil {
		return nil, fmt.Errorf("add app volume: %w", err)
	}
	return &v, nil
}

// RemoveVolume deletes a volume's row. Its directory is the caller's.
func (r *Repository) RemoveVolume(ctx context.Context, appID, volumeID int64) error {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM app_volumes WHERE id = $1 AND app_id = $2`, volumeID, appID)
	if err != nil {
		return fmt.Errorf("remove app volume: %w", err)
	}
	if err := affected(result); errors.Is(err, database.ErrNotFound) {
		return ErrVolumeNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// VolumeIDs is every volume there is, for telling kept data from live.
func (r *Repository) VolumeIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT id FROM app_volumes`)
	if err != nil {
		return nil, fmt.Errorf("list volume ids: %w", err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// TCPPortsFor reads the published ports of several apps at once, keyed by
// app.
func (r *Repository) TCPPortsFor(ctx context.Context, appIDs []int64) (map[int64][]TCPPort, error) {
	out := map[int64][]TCPPort{}
	if len(appIDs) == 0 {
		return out, nil
	}
	rows, err := r.q.QueryContext(ctx, `
		SELECT id, app_id, container_port, host_port, created_at
		FROM app_tcp_ports
		WHERE app_id = ANY($1)
		ORDER BY app_id, container_port`, appIDs)
	if err != nil {
		return nil, fmt.Errorf("list app tcp ports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p TCPPort
		if err := rows.Scan(&p.ID, &p.AppID, &p.ContainerPort, &p.HostPort, &p.CreatedAt); err != nil {
			return nil, err
		}
		out[p.AppID] = append(out[p.AppID], p)
	}
	return out, rows.Err()
}

// AddTCPPort records a container port published on a host port.
func (r *Repository) AddTCPPort(ctx context.Context, appID int64, containerPort, hostPort int) (*TCPPort, error) {
	var p TCPPort
	err := r.q.QueryRowContext(ctx, `
		INSERT INTO app_tcp_ports (app_id, container_port, host_port) VALUES ($1, $2, $3)
		RETURNING id, app_id, container_port, host_port, created_at`,
		appID, containerPort, hostPort).Scan(&p.ID, &p.AppID, &p.ContainerPort, &p.HostPort, &p.CreatedAt)
	switch {
	case database.UniqueViolationOn(err, "app_tcp_ports_host_port"):
		return nil, ErrTCPPortTaken
	case database.IsUniqueViolation(err):
		return nil, ErrTCPPortExists
	case err != nil:
		return nil, fmt.Errorf("add app tcp port: %w", err)
	}
	return &p, nil
}

// RemoveTCPPort deletes a published port's row.
func (r *Repository) RemoveTCPPort(ctx context.Context, appID, portID int64) error {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM app_tcp_ports WHERE id = $1 AND app_id = $2`, portID, appID)
	if err != nil {
		return fmt.Errorf("remove app tcp port: %w", err)
	}
	if err := affected(result); errors.Is(err, database.ErrNotFound) {
		return ErrTCPPortNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// UsedTCPPorts are the host ports apps are published on.
func (r *Repository) UsedTCPPorts(ctx context.Context) (map[int]bool, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT host_port FROM app_tcp_ports`)
	if err != nil {
		return nil, fmt.Errorf("list app tcp ports: %w", err)
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return nil, err
		}
		used[port] = true
	}
	return used, rows.Err()
}

func affected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ServerBySlug is a machine's id and whether it is the control plane.
func (r *Repository) ServerBySlug(ctx context.Context, slug string) (int64, bool, error) {
	var id int64
	var controlPlane bool
	err := r.q.QueryRowContext(ctx,
		`SELECT id, control_plane FROM nodes WHERE slug = $1`, slug).Scan(&id, &controlPlane)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ErrNoSuchNode
	}
	if err != nil {
		return 0, false, fmt.Errorf("find server %s: %w", slug, err)
	}
	return id, controlPlane, nil
}

// SetVolumeNode records that a volume's data is now on another machine.
func (r *Repository) SetVolumeNode(ctx context.Context, volumeID, nodeID int64) error {
	result, err := r.q.ExecContext(ctx,
		`UPDATE app_volumes SET node_id = $2 WHERE id = $1`, volumeID, nodeID)
	if err != nil {
		return fmt.Errorf("move app volume: %w", err)
	}
	if err := affected(result); errors.Is(err, database.ErrNotFound) {
		return ErrVolumeNotFound
	} else if err != nil {
		return err
	}
	return nil
}
