package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newEnvironmentCmd() *cobra.Command {
	environmentCmd := &cobra.Command{Use: "environment", Short: "Manage environments within a project"}

	var project string
	createCmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a new environment within a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			e, err := c.CreateEnvironment(context.Background(), project, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created environment %s in project %s\n", e.Slug, project)
			return nil
		},
	}
	createCmd.Flags().StringVar(&project, "project", "", "project slug")
	createCmd.MarkFlagRequired("project")

	var listProject string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List environments in a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			envs, err := c.ListEnvironments(context.Background(), listProject)
			if err != nil {
				return err
			}
			for _, e := range envs {
				fmt.Println(e.Slug)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&listProject, "project", "", "project slug")
	listCmd.MarkFlagRequired("project")

	var deleteProject string
	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: `Delete an environment and the apps in it`,
		Long: "Delete an environment and every app deployed in it — each\n" +
			"app's container is stopped and removed first.\n\n" +
			`The "production" environment can never be deleted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this deletes %s and every app in it for good; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteEnvironment(context.Background(), deleteProject, args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted environment %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&deleteConfirmed, "yes", false, "confirm the deletion")
	deleteCmd.Flags().StringVar(&deleteProject, "project", "", "project slug")
	deleteCmd.MarkFlagRequired("project")

	environmentCmd.AddCommand(createCmd, listCmd, deleteCmd, environmentEnvCommands())
	return environmentCmd
}
