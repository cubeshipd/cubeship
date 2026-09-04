package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"cubeship/internal/apiclient"
	"cubeship/internal/clicreds"

	"github.com/spf13/cobra"
)

func newAPIClient() (*apiclient.Client, error) {
	path, err := clicreds.DefaultPath()
	if err != nil {
		return nil, err
	}
	creds, err := clicreds.Load(path)
	if err != nil {
		return nil, err
	}
	return apiclient.New(creds.BaseURL, creds.Token), nil
}

func newAppCmd() *cobra.Command {
	appCmd := &cobra.Command{Use: "app", Short: "Manage Cubeship apps"}

	var domain, org, project, environment string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Register a new app and get its registry image path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			image, err := c.CreateApp(context.Background(), args[0], domain, org, project, environment)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s. Push to: %s\n", args[0], image)
			return nil
		},
	}
	createCmd.Flags().StringVar(&domain, "domain", "", "domain the app will be served on")
	createCmd.MarkFlagRequired("domain")
	createCmd.Flags().StringVar(&org, "org", "", "organization slug that will own this app")
	createCmd.MarkFlagRequired("org")
	createCmd.Flags().StringVar(&project, "project", "", "project slug this app belongs to")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().StringVar(&environment, "env", "", `environment slug within the project (default "production")`)

	var tag string
	deployCmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Manually redeploy an app from the given (or latest) image tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.Deploy(context.Background(), args[0], tag); err != nil {
				return err
			}
			fmt.Printf("Deployed %s\n", args[0])
			return nil
		},
	}
	deployCmd.Flags().StringVar(&tag, "tag", "latest", "image tag to deploy")

	logsCmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Stream an app's container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rc, err := c.Logs(context.Background(), args[0])
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}

	envCmd := &cobra.Command{Use: "env", Short: "Manage an app's environment variables"}
	envSetCmd := &cobra.Command{
		Use:   "set <name> KEY=VALUE [KEY=VALUE...]",
		Short: "Set environment variables for an app",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := parseEnvPairs(args[1:])
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.SetEnv(context.Background(), args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Updated env for %s\n", args[0])
			return nil
		},
	}
	envCmd.AddCommand(envSetCmd)

	appCmd.AddCommand(createCmd, deployCmd, logsCmd, envCmd)
	return appCmd
}
