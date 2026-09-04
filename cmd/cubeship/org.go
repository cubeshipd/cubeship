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
			if _, err := c.CreateOrg(context.Background(), slug, args[0]); err != nil {
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

	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete an organization",
		Long: "Delete an organization and its memberships.\n\n" +
			"The users themselves stay — they may belong to other\n" +
			"organizations. Refused while any project remains. Super-admin only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this deletes %s for good; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteOrg(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted organization %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&deleteConfirmed, "yes", false, "confirm that the organization should be deleted")
	orgCmd.AddCommand(deleteCmd)

	orgCmd.AddCommand(createCmd, listCmd)
	return orgCmd
}
