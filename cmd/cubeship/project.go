package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{Use: "project", Short: "Manage Cubeship projects"}

	var org, slug string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: `Create a new project (comes with a "production" environment)`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			p, err := c.CreateProject(context.Background(), org, slug, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created project %q (slug: %s) with environment(s): %s\n", args[0], p.Slug, strings.Join(p.Environments, ", "))
			return nil
		},
	}
	createCmd.Flags().StringVar(&org, "org", "", "organization slug the project belongs to")
	createCmd.MarkFlagRequired("org")
	createCmd.Flags().StringVar(&slug, "slug", "", "short identifier used in URLs")
	createCmd.MarkFlagRequired("slug")

	var listOrg string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects in an organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			projects, err := c.ListProjects(context.Background(), listOrg)
			if err != nil {
				return err
			}
			for _, p := range projects {
				fmt.Printf("%s\t%s\n", p.Slug, p.Name)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listOrg, "org", "", "organization slug")
	listCmd.MarkFlagRequired("org")

	var envOrg string
	envCmd := &cobra.Command{Use: "env", Short: "Manage a project's environment variables"}
	envSetCmd := &cobra.Command{
		Use:   "set <project-slug> KEY=VALUE [KEY=VALUE...]",
		Short: "Set environment variables inherited by every environment (and app) in this project",
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
			if err := c.SetProjectEnv(context.Background(), envOrg, args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Updated env for project %s\n", args[0])
			return nil
		},
	}
	envSetCmd.Flags().StringVar(&envOrg, "org", "", "organization slug")
	envSetCmd.MarkFlagRequired("org")
	envCmd.AddCommand(envSetCmd)

	projectCmd.AddCommand(createCmd, listCmd, envCmd)
	return projectCmd
}

// parseEnvPairs parses a list of "KEY=VALUE" strings, the shape `app env
// set` and `project/environment env set` all take.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	vars := map[string]string{}
	for _, kv := range pairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid KEY=VALUE pair: %q", kv)
		}
		vars[parts[0]] = parts[1]
	}
	return vars, nil
}
