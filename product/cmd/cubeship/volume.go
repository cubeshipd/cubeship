package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newAppVolumeCmd is `cubeship app volume`.
func newAppVolumeCmd() *cobra.Command {
	volumeCmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage an app's volumes: directories that outlive its container",
		Long: "A volume is a directory mounted into an app's container whose contents\n" +
			"survive deploys and restarts.\n\n" +
			"An app with a volume runs as one copy on the machine its data is on, and\n" +
			"each deploy stops the old container before the new one starts — two\n" +
			"containers on one directory corrupt it — so the app is briefly\n" +
			"unavailable during a deploy.",
	}

	listCmd := &cobra.Command{
		Use:   "list <app>",
		Short: "List an app's volumes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			volumes, err := c.ListAppVolumes(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(volumes) == 0 {
				fmt.Println("No volumes. Everything this app writes is gone on its next deploy.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tPATH\tSERVER")
			for _, v := range volumes {
				fmt.Fprintf(w, "%d\t%s\t%s\n", v.ID, v.Path, v.Node)
			}
			return w.Flush()
		},
	}

	addCmd := &cobra.Command{
		Use:   "add <app> <path>",
		Short: "Give an app a volume at a path inside its container",
		Long: "Give an app a volume at <path>, an absolute path inside its container.\n\n" +
			"It is mounted from the next deploy. Refused while the app runs as more\n" +
			"than one copy, on more than one server, or autoscales.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			v, err := c.AddAppVolume(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("Added volume %d at %s on %s. Deploy %s to mount it.\n", v.ID, v.Path, v.Node, args[0])
			return nil
		},
	}

	var confirmed, deleteData bool
	removeCmd := &cobra.Command{
		Use:   "remove <app> <id>",
		Short: "Take a volume off an app, keeping its data unless --delete-data",
		Long: "Take a volume off an app. Its data is kept on the server unless\n" +
			"--delete-data is given, which cannot be undone. The container running\n" +
			"now keeps the directory until the next deploy.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid volume id %q", args[1])
			}
			if !confirmed {
				what := "keeps its data"
				if deleteData {
					what = "deletes its data for good"
				}
				return fmt.Errorf("this takes volume %d off %s and %s; pass --yes to confirm", id, args[0], what)
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.RemoveAppVolume(context.Background(), args[0], id, deleteData); err != nil {
				return err
			}
			if deleteData {
				fmt.Printf("Removed volume %d and deleted its data\n", id)
			} else {
				fmt.Printf("Removed volume %d; its data is kept\n", id)
			}
			return nil
		},
	}
	removeCmd.Flags().BoolVar(&confirmed, "yes", false, "confirm the removal")
	removeCmd.Flags().BoolVar(&deleteData, "delete-data", false, "delete the volume's data too, which cannot be undone")

	volumeCmd.AddCommand(listCmd, addCmd, removeCmd)
	return volumeCmd
}
