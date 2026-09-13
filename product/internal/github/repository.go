package github

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes installations. A thin value over a
// Queryer, like every other repository here.
type Repo struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repo { return &Repo{q: q} }

const columns = `id, installation_id, account_login, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Installation, error) {
	var i Installation
	if err := row.Scan(&i.ID, &i.GitHubID, &i.Account, &i.CreatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

// Upsert records an installation, replacing whatever that GitHub
// installation was tied to before.
//
// GitHub reuses an installation id when an App is reinstalled on the
// same account, so a reinstall replaces the row rather than colliding
// with it: refusing would leave someone with an installation they can
// neither use nor replace.
func (r *Repo) Upsert(ctx context.Context, installationID int64, account string) (*Installation, error) {
	row := r.q.QueryRowContext(ctx, `
		INSERT INTO github_installations (installation_id, account_login)
		VALUES ($1, $2)
		ON CONFLICT (installation_id) DO UPDATE
		SET account_login = EXCLUDED.account_login
		RETURNING `+columns, installationID, account)
	i, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("record the installation: %w", err)
	}
	return i, nil
}

func (r *Repo) List(ctx context.Context) ([]*Installation, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM github_installations ORDER BY account_login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Installation
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ForAccount is the clone-time lookup: an organization, the GitHub
// account a repository belongs to, and whether there is an installation
// joining the two.
func (r *Repo) ForAccount(ctx context.Context, account string) (*Installation, bool, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM github_installations WHERE lower(account_login) = lower($1)`, account)
	i, err := scan(row)
	if err != nil {
		return nil, false, nil
	}
	return i, true, nil
}

// ByGitHubID resolves an installation the way a webhook names it.
func (r *Repo) ByGitHubID(ctx context.Context, installationID int64) (*Installation, bool, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM github_installations WHERE installation_id = $1`, installationID)
	i, err := scan(row)
	if err != nil {
		return nil, false, nil
	}
	return i, true, nil
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM github_installations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete the installation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete the installation: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// DeleteByGitHubID removes an installation GitHub says is gone. It is
// not scoped to an organization: the event is GitHub telling us the
// grant no longer exists, whoever held it.
func (r *Repo) DeleteByGitHubID(ctx context.Context, installationID int64) error {
	if _, err := r.q.ExecContext(ctx,
		`DELETE FROM github_installations WHERE installation_id = $1`, installationID); err != nil {
		return fmt.Errorf("delete the installation: %w", err)
	}
	return nil
}
