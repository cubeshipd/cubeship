package main

import (
	"context"
	"fmt"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

// newAppHookCmd is `cubeship app hook`: the URL a CI posts to when it
// has pushed an image somewhere Cubeship cannot see.
func newAppHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook <app>",
		Short: "Print the URL that deploys an app when something posts to it",
		Long: "Print an app's deploy webhook.\n\n" +
			"Every app has one. POST to it — no API key, the token in the\n" +
			"URL is the authorization, so keep it secret — and the app\n" +
			"deploys. An app that runs a published image takes ?tag=1.2.3,\n" +
			"which an external app saves; one that builds takes ?ref=main.\n" +
			"?wait=true answers once the deploy has finished.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			hook, err := c.DeployHook(context.Background(), args[0])
			if err != nil {
				return err
			}
			printHook(hook)
			return nil
		},
	}

	var confirmed bool
	rotateCmd := &cobra.Command{
		Use:   "rotate <app>",
		Short: "Give an app's deploy webhook a new URL; the old one stops working",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("the current webhook URL of %s stops working at once; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			hook, err := c.RotateDeployHook(context.Background(), args[0])
			if err != nil {
				return err
			}
			printHook(hook)
			return nil
		},
	}
	rotateCmd.Flags().BoolVar(&confirmed, "yes", false, "confirm that the old URL should stop working")
	cmd.AddCommand(rotateCmd)
	return cmd
}

func printHook(hook client.DeployHook) {
	if hook.URL == "" {
		fmt.Printf("Token: %s\nThis instance has no domain yet: POST to https://<its domain>/hooks/deploy/<token> once it has one.\n", hook.Token)
		return
	}
	fmt.Println(hook.URL)
}
