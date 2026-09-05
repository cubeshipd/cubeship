package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{Use: "project", Short: "Manage Cubeship projects"}

	createCmd := &cobra.Command{
		Use:   "create <slug>",
		Short: `Create a new project (comes with a "production" environment)`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			p, err := c.CreateProject(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created project %s with environment(s): %s\n", p.Slug, strings.Join(p.Environments, ", "))
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects in an organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			projects, err := c.ListProjects(context.Background())
			if err != nil {
				return err
			}
			for _, p := range projects {
				fmt.Println(p.Slug)
			}
			return nil
		},
	}

	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <project-slug>",
		Short: "Delete a project and everything in it",
		Long: "Delete a project, every environment inside it and every app in\n" +
			"those — each app's container is stopped and removed first.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this deletes %s, its environments and every app in them for good; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteProject(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted project %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&deleteConfirmed, "yes", false, "confirm that the project should be deleted")

	projectCmd.AddCommand(createCmd, listCmd, deleteCmd, projectEnvCommands())
	return projectCmd
}
