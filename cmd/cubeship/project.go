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

	projectCmd.AddCommand(createCmd, listCmd, projectEnvCommands())
	return projectCmd
}
