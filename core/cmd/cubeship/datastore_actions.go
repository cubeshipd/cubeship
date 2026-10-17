package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newDatastoreAttachCmd() *cobra.Command {
	var appName, prefix string
	cmd := &cobra.Command{
		Use:   "attach <name>",
		Short: "Give an app this database's connection variables",
		Long: "Attach an app to a database.\n\n" +
			"The app's container is given DATABASE_URL and its parts from its\n" +
			"next deploy onwards — a container keeps the environment it was\n" +
			"created with, so nothing changes until you deploy it.\n\n" +
			"The app is named by its full reference, project/environment/app,\n" +
			"and may be in any project. Use --prefix when one app needs a\n" +
			"second database.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.AttachDatastore(context.Background(), args[0], appName, prefix)
			if err != nil {
				return err
			}
			for _, a := range d.Attachments {
				if a.App != appName {
					continue
				}
				fmt.Printf("Attached %s to %s. It will receive:\n  %s\n",
					appName, d.Name, strings.Join(a.Variables, "\n  "))
			}
			fmt.Printf("\nDeploy %s for its container to pick them up.\n", appName)
			return nil
		},
	}
	cmd.Flags().StringVar(&appName, "app", "", "the app's full reference: project/environment/app")
	cmd.MarkFlagRequired("app")
	cmd.Flags().StringVar(&prefix, "prefix", "", `name the variables under a prefix, e.g. "ANALYTICS_"`)
	return cmd
}

func newDatastoreDetachCmd() *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "detach <name>",
		Short: "Stop giving an app this database's variables",
		Long: "Detach an app from a database.\n\n" +
			"Its container keeps the variables it was created with until it is\n" +
			"deployed again, so this is not how you cut an app off in a hurry.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if _, err := c.DetachDatastore(context.Background(), args[0], appName); err != nil {
				return err
			}
			fmt.Printf("Detached %s from %s. Deploy it for the change to reach its container.\n", appName, args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&appName, "app", "", "the app's full reference: project/environment/app")
	cmd.MarkFlagRequired("app")
	return cmd
}

func newDatastoreLogsCmd() *cobra.Command {
	var tail string
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Print what the database engine has written",
		Long: "Print a database's own log.\n\n" +
			"The first place to look when it refuses connections or will\n" +
			"not start. It carries no credential — the engine prints its own\n" +
			"startup, not what Cubeship configured it with.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rc, err := c.DatastoreLogs(context.Background(), args[0], tail)
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}
	cmd.Flags().StringVar(&tail, "tail", "", `number of trailing lines, or "all" (default: the daemon's own limit)`)
	return cmd
}

func newDatastoreStopCmd() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Turn a database off, keeping it and its data",
		Long: "Turn a database off.\n\n" +
			"The container stops and stays, so its log survives and its data\n" +
			"is untouched. `db start` brings it back.\n\n" +
			"Every attached app keeps running and starts failing to connect,\n" +
			"which is why this asks.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("apps attached to %s will start failing to connect; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.StopDatastore(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%s is %s. Bring it back with: cubeship db start %s\n", d.Name, d.Status, d.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirmed, "yes", false, "confirm that the database should be turned off")
	return cmd
}

func newDatastoreStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Turn a stopped database back on",
		Long: "Turn a database back on.\n\n" +
			"It is provisioned again — the container is recreated from the\n" +
			"same settings, and the data is a directory on the host that a\n" +
			"recreate does not touch. This is also how a database whose\n" +
			"provisioning failed is retried.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.StartDatastore(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%s is %s. `cubeship db get %s` says how it went.\n", d.Name, d.Status, d.Name)
			return nil
		},
	}
}

func newDatastoreExposeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "expose <name>",
		Short: "Publish a database on a host port",
		Long: "Publish a database on a host port, so something that is not an app\n" +
			"on this instance can connect to it.\n\n" +
			"There is no TLS in front of it. A database speaks its own protocol\n" +
			"on its own port, so an exposed one is a password on the open\n" +
			"internet — what makes that safe is the password and a firewall\n" +
			"rule, which is yours to write.\n\n" +
			"The container is replaced to pick the port up. The data is a bind\n" +
			"mount on the host and survives that untouched.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.ExposeDatastore(context.Background(), args[0], port)
			if err != nil {
				return err
			}
			where := d.ExternalHost
			if where == "" {
				where = "this host"
			}
			fmt.Printf("%s is being republished on %s:%d.\n", d.Name, where, d.ExposedPort)
			fmt.Println("Open that port in your firewall for it to be reachable.")
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "which host port to publish on; 0 takes the next free one from 15000-15999")
	return cmd
}

func newDatastoreUnexposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unexpose <name>",
		Short: "Take a database off its host port",
		Long: "Take a database off its host port.\n\n" +
			"Apps on this instance are unaffected — they never used it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if _, err := c.UnexposeDatastore(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("%s is no longer published outside this instance.\n", args[0])
			return nil
		},
	}
}

func newDatastoreDeleteCmd() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a database, its container and its data",
		Long: "Delete a database.\n\n" +
			"The container is stopped and removed, and the data directory on\n" +
			"the host goes with it. There is no backup and this cannot be\n" +
			"undone.\n\n" +
			"The apps attached to it keep running: a container holds the\n" +
			"environment it was created with, so they only lose DATABASE_URL\n" +
			"the next time they are deployed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("this deletes %s and everything in it, permanently; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteDatastore(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted %s and the data behind it.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirmed, "yes", false, "confirm that the database and its data should be deleted")
	return cmd
}

// newDatastoreLimitsCmd is `cubeship db limits`.
func newDatastoreLimitsCmd() *cobra.Command {
	var cpu, memory string
	cmd := &cobra.Command{
		Use:   "limits <name>",
		Short: "Cap what a database may use",
		Long: "Cap how much of the machine a database's container may take.\n\n" +
			"--cpu is cores and may be fractional: 0.5 is half a core.\n" +
			"--memory takes a size: 512Mi, 2Gi, 1500M. It is enforced by\n" +
			"the kernel killing whatever crosses it, so lowering one below\n" +
			"what the database is already holding kills it on the spot —\n" +
			"which its clients see as the connection going away.\n\n" +
			"This is the container on a box this size most worth capping.\n" +
			"An app that leaks is one app; a database that takes every\n" +
			"page of memory takes the daemon and the proxy with it.\n\n" +
			"Changing a limit takes effect immediately and does not\n" +
			"replace the container, unlike publishing a port. Removing\n" +
			"one — passing 0 — waits for the next start, because Docker\n" +
			"reads a zero as \"leave that one alone\".\n\n" +
			"With no flags it prints what the database is capped at now.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.GetDatastore(context.Background(), args[0])
			if err != nil {
				return err
			}
			if cpu == "" && memory == "" {
				fmt.Println(describeLimits(d.Limits))
				return nil
			}

			// Whatever is not being changed is sent as it stands: the
			// ceiling travels whole, so leaving a flag off must not
			// read as removing that half.
			l := d.Limits
			if cpu != "" {
				if l.CPU, err = strconv.ParseFloat(cpu, 64); err != nil {
					return fmt.Errorf("--cpu takes a number of cores, like 0.5 or 2: %w", err)
				}
			}
			if memory != "" {
				if l.Memory, err = parseSize(memory); err != nil {
					return err
				}
			}

			capped, err := c.SetDatastoreLimits(context.Background(), args[0], l)
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s\n", capped.Name, describeLimits(capped.Limits))
			if capped.Limits.CPU == 0 || capped.Limits.Memory == 0 {
				fmt.Println("A limit removed here takes effect on the next start; one changed is already in force.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cpu, "cpu", "", "cores it may use, fractional allowed; 0 removes the limit")
	cmd.Flags().StringVar(&memory, "memory", "", "memory it may hold, e.g. 512Mi or 2Gi; 0 removes the limit")
	return cmd
}
