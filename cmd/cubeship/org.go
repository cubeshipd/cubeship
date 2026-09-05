package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCmd() *cobra.Command {
	orgCmd := &cobra.Command{Use: "org", Short: "Manage Cubeship organizations"}

	createCmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a new organization (super-admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if _, err := c.CreateOrg(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Created organization %s\n", args[0])
			return nil
		},
	}

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
				fmt.Println(o.Slug)
			}
			return nil
		},
	}

	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete an organization",
		Long: "Delete an organization and everything inside it.\n\n" +
			"Every app in it is stopped and removed, then its projects,\n" +
			"environments and memberships. The users themselves stay — they\n" +
			"may belong to other organizations. Super-admin only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this deletes %s and everything in it for good; pass --yes to confirm", args[0])
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
