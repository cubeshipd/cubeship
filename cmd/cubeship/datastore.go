package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

// newDatastoreCmd is `cubeship db`.
//
// A managed database is addressed the way an app is, because it lives in
// the same place: project/environment/name, with two parts meaning
// production.
func newDatastoreCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:     "db",
		Aliases: []string{"datastore"},
		Short:   "Manage the databases Cubeship runs",
		Long: "Manage the databases Cubeship runs for the apps on this instance.\n\n" +
			"A database is named by its reference: project/environment/name.\n" +
			"Two parts — project/name — means the production environment. It\n" +
			"lives in an environment beside the apps that use it, so staging\n" +
			"and production hold different data without anybody spelling that\n" +
			"into a name.\n\n" +
			"Apps reach it by being attached to it: `db attach` gives an app\n" +
			"DATABASE_URL and its parts, from its next deploy onwards.",
	}

	dbCmd.AddCommand(
		newDatastoreCreateCmd(),
		newDatastoreListCmd(),
		newDatastoreGetCmd(),
		newDatastoreEnginesCmd(),
		newDatastoreCredentialsCmd(),
		newDatastoreAttachCmd(),
		newDatastoreDetachCmd(),
		newDatastoreExposeCmd(),
		newDatastoreUnexposeCmd(),
		newDatastoreDeleteCmd(),
	)
	return dbCmd
}

func newDatastoreCreateCmd() *cobra.Command {
	var spec client.DatastoreSpec
	var expose int
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Provision a database in a project's environment",
		Long: "Provision a database.\n\n" +
			"The password is generated unless you give one, and it is printed\n" +
			"here once — after this, `db credentials` is where to read it.\n\n" +
			"The container is pulled and started after this command returns,\n" +
			"so the database comes back as \"provisioning\". `db get` says how\n" +
			"it went.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			spec.Name = args[0]
			if cmd.Flags().Changed("expose") {
				spec.Expose = &expose
			}
			created, err := c.CreateDatastore(context.Background(), spec)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s (%s %s).\n", created.Reference, created.Engine, created.Version)
			fmt.Printf("  password: %s\n", created.Password)
			fmt.Printf("  reachable from apps at: %s:%d\n", created.Host, created.Port)
			if created.ExposedPort != 0 {
				fmt.Printf("  published on host port %d\n", created.ExposedPort)
			}
			fmt.Printf("\nAttach an app to it with: cubeship db attach %s --app <app>\n", created.Reference)
			return nil
		},
	}
	cmd.Flags().StringVar(&spec.Project, "project", "", "project slug this database belongs to")
	cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&spec.Environment, "env", "", `environment slug within the project (default "production")`)
	cmd.Flags().StringVar(&spec.Engine, "engine", "", "postgres, mysql or mariadb — see `db engines`")
	cmd.MarkFlagRequired("engine")
	cmd.Flags().StringVar(&spec.Version, "version", "", "a version the daemon offers for that engine (default: the newest). Permanent")
	cmd.Flags().StringVar(&spec.Username, "username", "", `the login to create (default "cubeship")`)
	cmd.Flags().StringVar(&spec.Password, "password", "", "the password to set (default: generated)")
	cmd.Flags().StringVar(&spec.Database, "database", "", "the database to create inside the server (default: the name, with underscores)")
	cmd.Flags().StringVar(&spec.Description, "description", "", "what this database is for")
	cmd.Flags().IntVar(&expose, "expose", 0, "publish on a host port; 0 picks one. Off unless the flag is given — see `db expose`")
	return cmd
}

func newDatastoreListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the databases on this instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			all, err := c.ListDatastores(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "REFERENCE\tENGINE\tSTATUS\tEXPOSED\tATTACHED")
			for _, d := range all {
				exposed := "-"
				if d.ExposedPort != 0 {
					exposed = fmt.Sprintf("%d", d.ExposedPort)
				}
				fmt.Fprintf(w, "%s\t%s %s\t%s\t%s\t%s\n",
					d.Reference, d.Engine, d.Version, d.Status, exposed, attachedList(d))
			}
			return w.Flush()
		},
	}
}

// attachedList names the apps wired to a database, for a column with
// room for one line. Empty is worth reading: a database nothing is
// attached to is one no app can reach.
func attachedList(d client.Datastore) string {
	if len(d.Attachments) == 0 {
		return "-"
	}
	names := make([]string, 0, len(d.Attachments))
	for _, a := range d.Attachments {
		names = append(names, a.App)
	}
	return strings.Join(names, ", ")
}

func newDatastoreGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <datastore>",
		Short: "Show one database, and which apps are attached to it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.GetDatastore(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Reference:   %s\n", d.Reference)
			fmt.Printf("Engine:      %s %s\n", d.Engine, d.Version)
			fmt.Printf("Status:      %s\n", d.Status)
			if d.Error != "" {
				fmt.Printf("Error:       %s\n", d.Error)
			}
			fmt.Printf("Username:    %s\n", d.Username)
			if d.Database != "" {
				fmt.Printf("Database:    %s\n", d.Database)
			}
			fmt.Printf("From apps:   %s:%d\n", d.Host, d.Port)
			if d.ExposedPort != 0 {
				fmt.Printf("From outside: %s:%d\n", d.ExternalHost, d.ExposedPort)
			}
			if len(d.Attachments) == 0 {
				fmt.Println("\nNo apps attached. Nothing on this instance can reach it yet.")
				return nil
			}
			fmt.Println("\nAttached apps:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  APP\tVARIABLES")
			for _, a := range d.Attachments {
				fmt.Fprintf(w, "  %s\t%s\n", a.App, strings.Join(a.Variables, ", "))
			}
			return w.Flush()
		},
	}
}

func newDatastoreEnginesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "engines",
		Short: "List the engines this daemon can run, and their versions",
		Long: "List the engines this daemon can run.\n\n" +
			"Read this rather than guessing a version: a version is permanent\n" +
			"once a database runs it, so the daemon only offers the ones it\n" +
			"will keep running.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			engines, err := c.ListDatastoreEngines(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ENGINE\tDEFAULT\tVERSIONS\tPORT")
			for _, e := range engines {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
					e.Engine, e.DefaultVersion, strings.Join(e.Versions, ", "), e.Port)
			}
			return w.Flush()
		},
	}
}

func newDatastoreCredentialsCmd() *cobra.Command {
	var quiet bool
	cmd := &cobra.Command{
		Use:     "credentials <datastore>",
		Aliases: []string{"creds"},
		Short:   "Print the login and connection strings for a database",
		Long: "Print a database's login.\n\n" +
			"This writes a password to your terminal, and to your shell's\n" +
			"history if you pipe it somewhere. It needs the admin role.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			creds, err := c.DatastoreCredentials(context.Background(), args[0])
			if err != nil {
				return err
			}
			// --uri is for `psql "$(cubeship db creds ... --uri)"`,
			// where anything else on stdout is in the way.
			if quiet {
				if creds.ExternalURI != "" {
					fmt.Println(creds.ExternalURI)
					return nil
				}
				fmt.Println(creds.InternalURI)
				return nil
			}
			fmt.Printf("Username: %s\n", creds.Username)
			fmt.Printf("Password: %s\n", creds.Password)
			if creds.Database != "" {
				fmt.Printf("Database: %s\n", creds.Database)
			}
			fmt.Printf("\nFrom an app on this instance:\n  %s\n", creds.InternalURI)
			if creds.ExternalURI != "" {
				fmt.Printf("\nFrom anywhere else:\n  %s\n", creds.ExternalURI)
			} else {
				fmt.Printf("\nNot reachable from outside this instance. `cubeship db expose %s` changes that.\n", args[0])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&quiet, "uri", false, "print only the connection string, for piping into a client")
	return cmd
}

func newDatastoreAttachCmd() *cobra.Command {
	var appName, prefix string
	cmd := &cobra.Command{
		Use:   "attach <datastore>",
		Short: "Give an app this database's connection variables",
		Long: "Attach an app to a database.\n\n" +
			"The app's container is given DATABASE_URL and its parts from its\n" +
			"next deploy onwards — a container keeps the environment it was\n" +
			"created with, so nothing changes until you deploy it.\n\n" +
			"The app has to be in the same environment as the database. Use\n" +
			"--prefix when one app needs a second database.",
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
					appName, d.Reference, strings.Join(a.Variables, "\n  "))
			}
			fmt.Printf("\nDeploy %s for its container to pick them up.\n", appName)
			return nil
		},
	}
	cmd.Flags().StringVar(&appName, "app", "", "the app's name within the same environment")
	cmd.MarkFlagRequired("app")
	cmd.Flags().StringVar(&prefix, "prefix", "", `name the variables under a prefix, e.g. "ANALYTICS_"`)
	return cmd
}

func newDatastoreDetachCmd() *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "detach <datastore>",
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
	cmd.Flags().StringVar(&appName, "app", "", "the app's name within the same environment")
	cmd.MarkFlagRequired("app")
	return cmd
}

func newDatastoreExposeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "expose <datastore>",
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
			fmt.Printf("%s is being republished on %s:%d.\n", d.Reference, where, d.ExposedPort)
			fmt.Println("Open that port in your firewall for it to be reachable.")
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "which host port to publish on; 0 takes the next free one from 15000-15999")
	return cmd
}

func newDatastoreUnexposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unexpose <datastore>",
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
		Use:   "delete <datastore>",
		Short: "Delete a database, its container and its data",
		Long: "Delete a database.\n\n" +
			"The container is stopped and removed, and the data directory on\n" +
			"the host goes with it. There is no backup and this cannot be\n" +
			"undone.",
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
