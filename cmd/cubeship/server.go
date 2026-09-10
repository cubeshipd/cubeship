package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"cubeship/internal/cli/creds"

	"github.com/spf13/cobra"
)

// newServerCmd is `cubeship server`.
//
// The machines this instance is made of: the control plane somebody
// installed, and the workers it manages. The control plane is a row
// like the others, because a listing of "the other servers" is one that
// cannot answer where anything runs.
func newServerCmd() *cobra.Command {
	serverCmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"servers", "node"},
		Short:   "Manage the machines this instance is made of",
		Long: "Manage the machines this instance is made of.\n\n" +
			"An instance is a control plane and any number of workers. The\n" +
			"control plane is the box you installed — the database, the\n" +
			"dashboard, the registry, the builder, and every decision. A\n" +
			"worker is a second box running the same image in a mode where it\n" +
			"decides nothing: it dials home every ten seconds, is told what to\n" +
			"run, and publishes no port of its own.\n\n" +
			"Adding one here mints a credential and contacts nothing. The row\n" +
			"is a place for a machine that does not exist yet; installing the\n" +
			"agent on it is the other half, and `server add` prints the command\n" +
			"to run there.",
	}

	serverCmd.AddCommand(
		newServerListCmd(),
		newServerGetCmd(),
		newServerAddCmd(),
		newServerRemoveCmd(),
	)
	return serverCmd
}

func newServerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the machines in this cluster",
		Long: "List the machines in this cluster, the control plane first.\n\n" +
			"A status is worked out from when the machine last called in,\n" +
			"never stored: `ready` is one that called within the last few\n" +
			"passes, `unreachable` is one that has missed three, and `pending`\n" +
			"is a machine that was added here and never installed.\n\n" +
			"MESH says whether the machine is on the cluster's private\n" +
			"network. One can be calling in and not on it, and then its\n" +
			"containers cannot reach the other machines'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			servers, err := c.ListServers(context.Background())
			if err != nil {
				return err
			}
			if len(servers) == 0 {
				fmt.Println("No servers.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tROLE\tADDRESS\tMESH\tVERSION")
			for _, s := range servers {
				role := "worker"
				if s.ControlPlane {
					role = "control plane"
				}
				mesh := "no"
				if s.InMesh {
					mesh = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					s.Name, s.Status, role, dash(s.Address), mesh, dash(s.Version))
			}
			return w.Flush()
		},
	}
}

func newServerGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show one machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			s, err := c.GetServer(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			role := "worker"
			if s.ControlPlane {
				role = "control plane"
			}
			fmt.Fprintf(w, "Name:\t%s\n", s.Name)
			if s.Description != "" {
				fmt.Fprintf(w, "Description:\t%s\n", s.Description)
			}
			fmt.Fprintf(w, "Role:\t%s\n", role)
			fmt.Fprintf(w, "Status:\t%s\n", s.Status)
			fmt.Fprintf(w, "Address:\t%s\n", dash(s.Address))
			fmt.Fprintf(w, "On the mesh:\t%t\n", s.InMesh)
			fmt.Fprintf(w, "Daemon:\t%s\n", dash(s.Version))
			fmt.Fprintf(w, "Cores:\t%d\n", s.Cores)
			fmt.Fprintf(w, "Memory:\t%s\n", bytesOr(s.MemoryTotalBytes))
			fmt.Fprintf(w, "Disk:\t%s\n", bytesOr(s.DiskTotalBytes))
			return w.Flush()
		},
	}
}

func newServerAddCmd() *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a machine and print how to install it",
		Long: "Add a machine to this cluster.\n\n" +
			"This mints the credential its agent authenticates with and\n" +
			"contacts nothing: the row is a place for a machine that does not\n" +
			"exist yet. What comes back is the command to run on that box,\n" +
			"which is the other half and the only one that touches it.\n\n" +
			"The credential is shown once. Only its hash is kept here, so\n" +
			"nothing can hand it back — a machine whose token was lost is\n" +
			"removed and added again.\n\n" +
			"The name is permanent: it is how every screen and every\n" +
			"placement refers to the machine.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			added, err := c.AddServer(context.Background(), args[0], description)
			if err != nil {
				return err
			}

			where, err := controlPlaneURL()
			if err != nil {
				return err
			}
			fmt.Printf("Added %s.\n\n", added.Name)
			fmt.Printf("Run this on that machine:\n\n")
			fmt.Printf("  curl -sSL https://cubeship.dev/install.sh | sh -s -- \\\n")
			fmt.Printf("    --control-plane %s \\\n", where)
			fmt.Printf("    --token %s\n\n", added.Token)
			fmt.Printf("The token is shown once — only its hash is stored here.\n")
			fmt.Printf("It appears in `cubeship server list` as soon as it calls in.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "what this machine is for")
	return cmd
}

func newServerRemoveCmd() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Take a machine out of this cluster",
		Long: "Take a machine out of this cluster.\n\n" +
			"A local act: the row and its credential go, and the agent is\n" +
			"refused the next time it calls. **Nothing on that machine is\n" +
			"touched** — the containers it is running keep running, and\n" +
			"uninstalling the daemon there is a separate thing you do on the\n" +
			"box. A delete that reached out would be one that hangs on a host\n" +
			"nobody can dial.\n\n" +
			"A machine with apps on it is refused. Where those apps should go\n" +
			"is a decision, and making it by deleting a row would make it\n" +
			"invisibly — move them with `app place` first.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("this removes %s from the cluster and revokes its credential; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.RemoveServer(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Removed %s. Nothing on that machine was touched.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirmed, "yes", false, "confirm that the machine should leave the cluster")
	return cmd
}

// controlPlaneURL is what a worker dials, which is the same address
// this CLI is signed in to. Read from the stored credentials rather
// than asked for: somebody adding a machine from here has already
// proved which instance they mean.
func controlPlaneURL() (string, error) {
	path, err := creds.DefaultPath()
	if err != nil {
		return "", err
	}
	saved, err := creds.Load(path)
	if err != nil {
		return "", err
	}
	return saved.BaseURL, nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// bytesOr renders a size a machine reported, or a dash for one that has
// not reported yet — a zero there is an absence, not a machine with no
// memory.
func bytesOr(n int64) string {
	if n == 0 {
		return "-"
	}
	const gib = 1 << 30
	return fmt.Sprintf("%.1f GiB", float64(n)/gib)
}
