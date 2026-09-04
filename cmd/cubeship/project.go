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

	var deleteOrg string
	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <project-slug>",
		Short: "Delete a project and the environments in it",
		Long: "Delete a project and every environment inside it.\n\n" +
			"Refused while any app still lives in the project — delete those\n" +
			"first, since removing an app means stopping its container.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this deletes %s and its environments for good; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteProject(context.Background(), deleteOrg, args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted project %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteOrg, "org", "", "organization slug")
	deleteCmd.MarkFlagRequired("org")
	deleteCmd.Flags().BoolVar(&deleteConfirmed, "yes", false, "confirm that the project should be deleted")

	projectCmd.AddCommand(createCmd, listCmd, deleteCmd, projectEnvCommands())
	return projectCmd
}
