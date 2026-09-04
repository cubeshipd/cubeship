package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"cubeship/internal/clicreds"

	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login <daemon-url> <token>",
		Short: "Save credentials for talking to a Cubeship daemon",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := clicreds.DefaultPath()
			if err != nil {
				return err
			}
			creds := clicreds.Credentials{BaseURL: strings.TrimRight(args[0], "/"), Token: args[1]}
			if err := clicreds.Save(path, creds); err != nil {
				return err
			}
			fmt.Printf("Saved credentials to %s\n", path)
			return nil
		},
	}
}

func newRegistryCmd() *cobra.Command {
	registryCmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage the Cubeship container registry",
	}
	var password string
	registryLoginCmd := &cobra.Command{
		Use:   "login",
		Short: "Run 'docker login' against the Cubeship registry",
		// The registry credential is still instance-wide (one htpasswd
		// account), not per-user: its password is the daemon's system
		// token from $CUBESHIP_DATA_DIR/token on the server, which is
		// not the same thing as your API key. Per-org registry
		// authorization arrives with the follow-up registry-token work;
		// until then --password is how a user who isn't the operator
		// logs in to push.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := clicreds.DefaultPath()
			if err != nil {
				return err
			}
			creds, err := clicreds.Load(path)
			if err != nil {
				return err
			}
			registryHost, err := clicreds.RegistryHostFromBaseURL(creds.BaseURL)
			if err != nil {
				return err
			}
			if password == "" {
				password = creds.Token
			}

			dockerLogin := exec.Command("docker", "login", registryHost, "-u", "cubeship", "--password-stdin")
			dockerLogin.Stdin = strings.NewReader(password)
			dockerLogin.Stdout = os.Stdout
			dockerLogin.Stderr = os.Stderr
			return dockerLogin.Run()
		},
	}
	registryLoginCmd.Flags().StringVar(&password, "password", "",
		"registry password (the daemon's token from $CUBESHIP_DATA_DIR/token); defaults to your saved API key")
	registryCmd.AddCommand(registryLoginCmd)
	return registryCmd
}
