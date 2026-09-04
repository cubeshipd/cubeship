package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newEnvironmentCmd() *cobra.Command {
	environmentCmd := &cobra.Command{Use: "environment", Short: "Manage environments within a project"}

	var org, project, slug string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new environment within a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			e, err := c.CreateEnvironment(context.Background(), org, project, slug, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created environment %q (slug: %s) in project %s\n", args[0], e.Slug, project)
			return nil
		},
	}
	createCmd.Flags().StringVar(&org, "org", "", "organization slug")
	createCmd.MarkFlagRequired("org")
	createCmd.Flags().StringVar(&project, "project", "", "project slug")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().StringVar(&slug, "slug", "", "short identifier used in URLs and as the environment name apps request")
	createCmd.MarkFlagRequired("slug")

	var listOrg, listProject string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List environments in a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			envs, err := c.ListEnvironments(context.Background(), listOrg, listProject)
			if err != nil {
				return err
			}
			for _, e := range envs {
				fmt.Printf("%s\t%s\n", e.Slug, e.Name)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listOrg, "org", "", "organization slug")
	listCmd.MarkFlagRequired("org")
	listCmd.Flags().StringVar(&listProject, "project", "", "project slug")
	listCmd.MarkFlagRequired("project")

	var deleteOrg, deleteProject string
	deleteCmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: `Delete an environment (the "production" environment can never be deleted)`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteEnvironment(context.Background(), deleteOrg, deleteProject, args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted environment %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteOrg, "org", "", "organization slug")
	deleteCmd.MarkFlagRequired("org")
	deleteCmd.Flags().StringVar(&deleteProject, "project", "", "project slug")
	deleteCmd.MarkFlagRequired("project")

	var envOrg, envProject string
	envCmd := &cobra.Command{Use: "env", Short: "Manage an environment's environment variables"}
	envSetCmd := &cobra.Command{
		Use:   "set <env-slug> KEY=VALUE [KEY=VALUE...]",
		Short: "Set environment variables inherited by every app in this environment",
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
			if err := c.SetEnvironmentEnv(context.Background(), envOrg, envProject, args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Updated env for environment %s\n", args[0])
			return nil
		},
	}
	envSetCmd.Flags().StringVar(&envOrg, "org", "", "organization slug")
	envSetCmd.MarkFlagRequired("org")
	envSetCmd.Flags().StringVar(&envProject, "project", "", "project slug")
	envSetCmd.MarkFlagRequired("project")
	envCmd.AddCommand(envSetCmd)

	environmentCmd.AddCommand(createCmd, listCmd, deleteCmd, envCmd)
	return environmentCmd
}
