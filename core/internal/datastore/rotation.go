package datastore

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/user"
)

// RotatePassword changes the live engine login before recording it, so
// a failed command never leaves the database row advertising a password
// the engine does not accept.
func (s *Service) RotatePassword(ctx context.Context, caller *user.User, name string) (Credentials, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return Credentials{}, err
	}
	if err := user.AllowSecrets(caller, user.ResDatabases, name); err != nil {
		return Credentials{}, err
	}
	mu := s.prov.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()

	d, err = s.Repo().ByID(ctx, d.ID)
	if err != nil {
		return Credentials{}, err
	}
	if d.ContainerID == "" || d.Status != StatusRunning {
		return Credentials{}, ErrNotRunning
	}
	next, err := GeneratePassword()
	if err != nil {
		return Credentials{}, err
	}
	if err := s.runPasswordRotation(ctx, d, next); err != nil {
		return Credentials{}, fmt.Errorf("change database password: %w", err)
	}
	if err := s.Repo().SetPassword(ctx, d.ID, next); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if rollbackErr := s.runPasswordRotation(rollbackCtx, d, d.Password); rollbackErr != nil {
			return Credentials{}, fmt.Errorf("save rotated password: %v; restore the previous password: %w", err, rollbackErr)
		}
		return Credentials{}, err
	}
	return s.Credentials(ctx, caller, name)
}

func passwordRotation(d *Datastore, next string) (cmd, env []string, input string) {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	switch d.Engine {
	case EnginePostgres:
		identifier := `"` + strings.ReplaceAll(d.Username, `"`, `""`) + `"`
		return []string{"psql", "-U", d.Username, "-d", d.Database, "-v", "ON_ERROR_STOP=1"},
			[]string{"PGPASSWORD=" + d.Password},
			"ALTER ROLE " + identifier + " WITH PASSWORD " + quote(next) + ";\n"
	case EngineMySQL, EngineMariaDB:
		client := "mysql"
		if d.Engine == EngineMariaDB {
			client = "mariadb"
		}
		return []string{client, "-u", d.Username}, []string{"MYSQL_PWD=" + d.Password},
			"ALTER USER " + quote(d.Username) + "@'%' IDENTIFIED BY " + quote(next) + ";\n"
	case EngineRedis:
		return []string{"redis-cli", "--raw"}, []string{"REDISCLI_AUTH=" + d.Password},
			"ACL SETUSER default resetpass >" + next + "\n"
	case EngineMongoDB:
		return []string{"mongosh", "--quiet", "admin"}, nil,
			"if (db.auth(" + strconv.Quote(d.Username) + ", " + strconv.Quote(d.Password) + ") !== 1) { throw new Error('authentication failed'); }\n" +
				"db.changeUserPassword(" + strconv.Quote(d.Username) + ", " + strconv.Quote(next) + ");\n"
	default:
		return nil, nil, ""
	}
}

func (s *Service) runPasswordRotation(ctx context.Context, d *Datastore, password string) error {
	cmd, env, input := passwordRotation(d, password)
	if len(cmd) == 0 {
		return fmt.Errorf("password rotation is not supported for %s", d.Engine)
	}
	_, err := s.Exec(ctx, d, cmd, env, strings.NewReader(input), io.Discard)
	return err
}
