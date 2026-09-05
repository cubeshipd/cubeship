package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{Use: "user", Short: "Manage Cubeship users"}

	var role string
	createCmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create an account and print its API key",
		Long: "Create an account on this instance.\n\n" +
			"The API key is printed once, here, and never again. There is no\n" +
			"password: an account gets one when it sets one.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			key, err := c.AddUser(context.Background(), args[0], role)
			if err != nil {
				return err
			}
			fmt.Printf("Created user %q (role: %s)\n", args[0], role)
			fmt.Printf("API key (shown once, save it now): %s\n", key)
			return nil
		},
	}
	createCmd.Flags().StringVar(&role, "role", "member", "admin or member")

	apiKeyCmd := &cobra.Command{Use: "api-key", Short: "Manage your own API keys"}
	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Replace the key you're currently using with a freshly generated one",
		Long: "Replace the API key this command is currently authenticating with.\n\n" +
			"Only that one key is affected — any other key you hold (one issued\n" +
			"to an MCP client via \"api-key create\", say) keeps working.",
		Args: cobra.NoArgs,
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

	createKeyCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Issue an additional, independent API key for yourself",
		Long: "Issue an additional API key for yourself, alongside any key you\n" +
			"already hold. This is how an MCP client (Claude Code, say) gets its\n" +
			"own credential, separate from the one your terminal uses — revoking\n" +
			"or rotating one never touches the other. Point the MCP client at\n" +
			"this daemon's /mcp endpoint with this key as its bearer token.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			_, key, err := c.CreateAPIKey(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("New API key %q (shown once, save it now): %s\n", args[0], key)
			return nil
		},
	}

	listKeysCmd := &cobra.Command{
		Use:   "list",
		Short: "List your own API keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			keys, err := c.ListAPIKeys(context.Background())
			if err != nil {
				return err
			}
			for _, k := range keys {
				current := ""
				if k.CurrentKey {
					current = "  (current)"
				}
				lastUsed := "never"
				if k.LastUsedAt != nil {
					lastUsed = k.LastUsedAt.Format("2006-01-02 15:04")
				}
				fmt.Printf("%d\t%s\tlast used: %s%s\n", k.ID, k.Name, lastUsed, current)
			}
			return nil
		},
	}

	revokeKeyCmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke one of your own API keys by id (from \"api-key list\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid key id %q", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.RevokeAPIKey(context.Background(), id); err != nil {
				return err
			}
			fmt.Printf("Revoked API key %d\n", id)
			return nil
		},
	}

	apiKeyCmd.AddCommand(rotateCmd, createKeyCmd, listKeysCmd, revokeKeyCmd)

	userCmd.AddCommand(createCmd, apiKeyCmd)
	return userCmd
}
