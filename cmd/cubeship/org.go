package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCmd() *cobra.Command {
	orgCmd := &cobra.Command{Use: "org", Short: "Manage Cubeship organizations"}

	var slug string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new organization (super-admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.CreateOrg(context.Background(), slug, args[0]); err != nil {
				return err
			}
			fmt.Printf("Created organization %q (slug: %s)\n", args[0], slug)
			return nil
		},
	}
	createCmd.Flags().StringVar(&slug, "slug", "", "short identifier used in URLs and registry paths")
	createCmd.MarkFlagRequired("slug")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List organizations you belong to (or all, if super-admin)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			orgs, err := c.ListOrgs(context.Background())
			if err != nil {
				return err
			}
			for _, o := range orgs {
				fmt.Printf("%s\t%s\n", o.Slug, o.Name)
			}
			return nil
		},
	}

	orgCmd.AddCommand(createCmd, listCmd)
	return orgCmd
}
