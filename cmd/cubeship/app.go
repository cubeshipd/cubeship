package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"cubeship/internal/cli/client"
	"cubeship/internal/cli/creds"

	"github.com/spf13/cobra"
)

func newAPIClient() (*client.Client, error) {
	path, err := creds.DefaultPath()
	if err != nil {
		return nil, err
	}
	saved, err := creds.Load(path)
	if err != nil {
		return nil, err
	}
	return client.New(saved.BaseURL, saved.Token), nil
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
			created, err := c.CreateApp(context.Background(), args[0], domain, org, project, environment)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s in %s/%s/%s. Push to: %s\n",
				created.Name, created.Org, created.Project, created.Environment, created.Image)
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

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the apps you can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			apps, err := c.ListApps(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tORG\tPROJECT\tENV\tSTATUS\tDOMAIN")
			for _, a := range apps {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					a.Name, a.Org, a.Project, a.Environment, a.Status, a.Domain)
			}
			return w.Flush()
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show one app, including its registry push path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			a, err := c.GetApp(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, row := range [][2]string{
				{"Name", a.Name}, {"Organization", a.Org}, {"Project", a.Project},
				{"Environment", a.Environment}, {"Domain", a.Domain},
				{"Status", a.Status}, {"Push to", a.Image},
			} {
				fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
			}
			return w.Flush()
		},
	}

	var tail string
	logsCmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Print an app's recent container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rc, err := c.Logs(context.Background(), args[0], tail)
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}
	logsCmd.Flags().StringVar(&tail, "tail", "", `number of trailing lines, or "all" (default: the daemon's own limit)`)

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
			if err := c.SetAppEnv(context.Background(), args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Updated env for %s\n", args[0])
			return nil
		},
	}
	envCmd.AddCommand(envSetCmd)

	appCmd.AddCommand(createCmd, listCmd, getCmd, deployCmd, logsCmd, envCmd)
	return appCmd
}
