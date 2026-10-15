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

	volumeCmd.AddCommand(listCmd, addCmd, removeCmd, newAppVolumeBackupCmd())
	return volumeCmd
}

// newAppVolumeBackupCmd is `cubeship app volume backup`.
func newAppVolumeBackupCmd() *cobra.Command {
	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up and restore a volume",
		Long: "Back up and restore one of an app's volumes. The app is stopped while a\n" +
			"copy is taken or put back, and started again afterwards. Requires the\n" +
			"admin role.",
	}

	listCmd := &cobra.Command{
		Use:   "list <app> <volume-id>",
		Short: "List a volume's backups, newest first",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseVolumeID(args[1])
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rows, err := c.ListVolumeBackups(context.Background(), args[0], id)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Println("No backups of this volume yet.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTAKEN\tWHERE\tSIZE\tSTATUS")
			for _, b := range rows {
				where := "this machine"
				if b.Store != "" {
					where = b.Store + "/" + b.Bucket
				}
				status := b.Status
				if b.Error != "" {
					status += ": " + b.Error
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", b.ID, b.StartedAt, where, b.Size, status)
			}
			return w.Flush()
		},
	}

	var takeStore, takeBucket string
	takeCmd := &cobra.Command{
		Use:   "take <app> <volume-id>",
		Short: "Back a volume up now, stopping the app for the copy",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseVolumeID(args[1])
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			b, err := c.TakeVolumeBackup(context.Background(), args[0], id, takeStore, takeBucket)
			if err != nil {
				return err
			}
			fmt.Printf("Backup %d started. Check it with: cubeship app volume backup list %s %d\n", b.ID, args[0], id)
			return nil
		},
	}

	takeCmd.Flags().StringVar(&takeStore, "store", "", "the S3 store linked from outside this instance to send it to; defaults to the volume's schedule's")
	takeCmd.Flags().StringVar(&takeBucket, "bucket", "", "the bucket in that store")

	var restoreServer string
	var confirmed bool
	restoreCmd := &cobra.Command{
		Use:   "restore <backup-id>",
		Short: "Replace a volume's data with a backup of it",
		Long: "Replace a volume's data with what it held when the backup was taken.\n" +
			"This cannot be undone. The app is stopped while it happens.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid backup id %q", args[0])
			}
			if !confirmed {
				return fmt.Errorf("this replaces the volume's data with backup %d and cannot be undone; pass --yes to confirm", id)
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.RestoreBackup(context.Background(), id, restoreServer); err != nil {
				return err
			}
			fmt.Printf("Restored backup %d\n", id)
			return nil
		},
	}
	restoreCmd.Flags().BoolVar(&confirmed, "yes", false, "confirm the restore")
	restoreCmd.Flags().StringVar(&restoreServer, "server", "", "restore on this server instead, which moves the app there; the copy on the old server is left")

	backupCmd.AddCommand(listCmd, takeCmd, restoreCmd)
	return backupCmd
}

func parseVolumeID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid volume id %q", s)
	}
	return id, nil
}
