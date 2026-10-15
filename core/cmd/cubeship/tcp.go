package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newAppTCPCmd is `cubeship app tcp`.
func newAppTCPCmd() *cobra.Command {
	tcpCmd := &cobra.Command{
		Use:   "tcp",
		Short: "Publish an app's TCP ports for protocols that are not HTTP",
		Long: "Publish a port of an app's container on a host port of the control plane,\n" +
			"for a protocol that is not HTTP: SSH into a Git server, a game server, a\n" +
			"broker. There is no TLS and no proxy in front of it.\n\n" +
			"An app with a published port runs as one copy on the control plane, and\n" +
			"each deploy stops the old container before the new one starts, so the\n" +
			"app is briefly unavailable during a deploy.",
	}

	listCmd := &cobra.Command{
		Use:   "list <app>",
		Short: "List an app's published TCP ports",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			ports, err := c.ListAppTCPPorts(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(ports) == 0 {
				fmt.Println("No TCP ports. The app is reached over HTTP at its domains only.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCONTAINER\tHOST")
			for _, p := range ports {
				fmt.Fprintf(w, "%d\t%d\t%d\n", p.ID, p.ContainerPort, p.HostPort)
			}
			return w.Flush()
		},
	}

	var hostPort int
	addCmd := &cobra.Command{
		Use:   "add <app> <container-port>",
		Short: "Publish a port of an app's container on the control plane",
		Long: "Publish <container-port> on a host port of the control plane, from the\n" +
			"next deploy. --host-port names it (1024-65535); without it one is picked\n" +
			"from 17000-17999. Refused while the app runs anywhere but the control\n" +
			"plane, as more than one copy, or autoscales.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			containerPort, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid container port %q", args[1])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			p, err := c.AddAppTCPPort(context.Background(), args[0], containerPort, hostPort)
			if err != nil {
				return err
			}
			fmt.Printf("Publishing port %d on host port %d. Deploy %s to publish it.\n", p.ContainerPort, p.HostPort, args[0])
			return nil
		},
	}
	addCmd.Flags().IntVar(&hostPort, "host-port", 0, "the host port to publish on; picked from 17000-17999 when left out")

	removeCmd := &cobra.Command{
		Use:   "remove <app> <id>",
		Short: "Stop publishing a TCP port of an app",
		Long: "Stop publishing a port. The container running now keeps it until the\n" +
			"next deploy.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid port id %q", args[1])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.RemoveAppTCPPort(context.Background(), args[0], id); err != nil {
				return err
			}
			fmt.Printf("Stopped publishing port %d from the next deploy of %s\n", id, args[0])
			return nil
		},
	}

	tcpCmd.AddCommand(listCmd, addCmd, removeCmd)
	return tcpCmd
}
