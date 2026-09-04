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
		Short: "Add a user to an organization, creating them if they are new",
		Long: "Add a user to an organization.\n\n" +
			"A new username creates the user and prints their API key, shown\n" +
			"once. An existing username is added to this organization as well —\n" +
			"users can belong to several — keeping the API key they already have.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.CreateOrgUser(context.Background(), org, args[0], role)
			if err != nil {
				return err
			}
			if key == "" {
				fmt.Printf("Added existing user %q to %s (role: %s)\n", args[0], org, role)
				fmt.Println("Their existing API key is unchanged.")
				return nil
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
