package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"cubeship/internal/apiclient"
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
	registryLoginCmd := &cobra.Command{
		Use:   "login",
		Short: "Run 'docker login' against the Cubeship registry as your own user",
		// Registry push/pull is per-user token auth now: your saved API
		// key is your registry password too, and the registry only
		// grants access to the orgs you actually belong to. No flags —
		// there's nothing to choose, unlike the old shared-credential
		// model.
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

			username, err := apiclient.New(creds.BaseURL, creds.Token).WhoAmI(context.Background())
			if err != nil {
				return fmt.Errorf("look up your username: %w", err)
			}

			dockerLogin := exec.Command("docker", "login", registryHost, "-u", username, "--password-stdin")
			dockerLogin.Stdin = strings.NewReader(creds.Token)
			dockerLogin.Stdout = os.Stdout
			dockerLogin.Stderr = os.Stderr
			return dockerLogin.Run()
		},
	}
	registryCmd.AddCommand(registryLoginCmd)
	return registryCmd
}
