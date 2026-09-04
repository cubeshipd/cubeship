package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{Use: "user", Short: "Manage Cubeship users"}

	var org, role string
	createCmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a user in an organization and print their API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.CreateOrgUser(context.Background(), org, args[0], role)
			if err != nil {
				return err
			}
			fmt.Printf("Created user %q in %s (role: %s)\n", args[0], org, role)
			fmt.Printf("API key (shown once, save it now): %s\n", key)
			return nil
		},
	}
	createCmd.Flags().StringVar(&org, "org", "", "organization slug")
	createCmd.MarkFlagRequired("org")
	createCmd.Flags().StringVar(&role, "role", "member", "role within the org: admin or member")

	apiKeyCmd := &cobra.Command{Use: "api-key", Short: "Manage your own API key"}
	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Revoke your current API key and issue a new one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.RotateAPIKey(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("New API key (shown once, save it now): %s\n", key)
			fmt.Println("Update your saved credentials: cubeship login <daemon-url> " + key)
			return nil
		},
	}
	apiKeyCmd.AddCommand(rotateCmd)

	userCmd.AddCommand(createCmd, apiKeyCmd)
	return userCmd
}
