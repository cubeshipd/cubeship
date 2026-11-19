package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/config"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// resetPassword issues a new password for an account and prints it once:
//
//	docker exec cubeship-daemon cubeshipd -reset-password <username>
//
// **It is the way back in when the admin is the one who forgot.** Every
// other reset is an admin's, through the API, and setup closed when the
// first account was made — so an instance whose only admin lost the
// password had nothing left but writing a hash into the database by
// hand. Running a command on the host is the proof here: whoever can
// already holds the Docker socket and the database's password.
//
// The rules are the admin's reset: the API keys stay, every session
// ends.
func resetPassword(username string, out io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Worker() {
		return errors.New("this machine is a worker and holds no accounts: run this on the control plane")
	}
	dsn, err := databaseURL(cfg)
	if err != nil {
		return err
	}
	db, err := database.Open(dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	password, err := user.NewService(db).RecoverPassword(context.Background(), username)
	if errors.Is(err, user.ErrNoSuchUser) {
		return fmt.Errorf("%q is not an account on this instance", username)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "New password for %s, shown once:\n\n  %s\n\n"+
		"Its API keys still work; every session it had is signed out.\n", username, password)
	return nil
}

// databaseURL is the database the running daemon uses, found the way
// ensureDatabase found it but **starting and creating nothing**: a new
// Postgres password written here would stop matching the database the
// next time the daemon starts.
func databaseURL(cfg *config.Config) (string, error) {
	if !cfg.ManagedDatabase() {
		return cfg.DatabaseURL, nil
	}
	path := filepath.Join(cfg.DataDir, pgPasswordFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the database password: %w", err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", fmt.Errorf("the database password in %s is empty", path)
	}
	return bootstrap.PostgresDSN(cfg, password), nil
}
