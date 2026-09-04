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
	registryCmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Run 'docker login' against the Cubeship registry",
		Args:  cobra.NoArgs,
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

			dockerLogin := exec.Command("docker", "login", registryHost, "-u", "cubeship", "--password-stdin")
			dockerLogin.Stdin = strings.NewReader(creds.Token)
			dockerLogin.Stdout = os.Stdout
			dockerLogin.Stderr = os.Stderr
			return dockerLogin.Run()
		},
	})
	return registryCmd
}
