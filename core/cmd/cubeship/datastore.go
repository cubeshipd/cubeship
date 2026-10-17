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
// A managed database belongs to the instance, so its name is the whole
// of its address — no project, no environment in front of it.
func newDatastoreCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:     "db",
		Aliases: []string{"datastore"},
		Short:   "Manage the databases Cubeship runs",
		Long: "Manage the databases Cubeship runs for the apps on this instance.\n\n" +
			"A database belongs to the instance rather than to a project: on\n" +
			"one host, one Postgres serving several small apps is the normal\n" +
			"shape, and those apps are routinely in different projects. Its\n" +
			"name is unique across the instance and is the whole of how it is\n" +
			"addressed.\n\n" +
			"Apps reach it by being attached to it: `db attach` gives an app\n" +
			"DATABASE_URL and its parts, from its next deploy onwards. The app\n" +
			"is named by its full reference, and may be in any project.\n\n" +
			"Nothing separates one environment's data from another's any more\n" +
			"— that is what the names are for: pg-production, pg-staging.",
	}

	dbCmd.AddCommand(
		newDatastoreCreateCmd(),
		newDatastoreListCmd(),
		newDatastoreGetCmd(),
		newDatastoreEnginesCmd(),
		newDatastoreExtensionCmd(),
		newDatastoreCredentialsCmd(),
		newDatastoreAttachCmd(),
		newDatastoreDetachCmd(),
		newDatastoreLogsCmd(),
		newDatastoreStopCmd(),
		newDatastoreStartCmd(),
		newDatastoreExposeCmd(),
		newDatastoreUnexposeCmd(),
		newDatastoreLimitsCmd(),
		newDatastoreDeleteCmd(),
	)
	return dbCmd
}

func newDatastoreCreateCmd() *cobra.Command {
	var spec client.DatastoreSpec
	var expose int
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Provision a database on this instance",
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
			fmt.Printf("Created %s (%s %s).\n", created.Name, created.Engine, created.Version)
			if len(created.Extensions) > 0 {
				// What came back, not what was asked for: the daemon adds
				// what an extension requires, so asking for vectorchord
				// creates pgvector too and this is where that is visible.
				fmt.Printf("  extensions: %s\n", strings.Join(created.Extensions, ", "))
			}
			fmt.Printf("  password: %s\n", created.Password)
			fmt.Printf("  reachable from apps at: %s:%d\n", created.Host, created.Port)
			if created.ExposedPort != 0 {
				fmt.Printf("  published on host port %d\n", created.ExposedPort)
			}
			fmt.Printf("\nAttach an app to it with: cubeship db attach %s --app <project/env/app>\n", created.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&spec.Engine, "engine", "", "postgres, mysql or mariadb — see `db engines`")
	cmd.MarkFlagRequired("engine")
	cmd.Flags().StringVar(&spec.Version, "version", "", "a version the daemon offers for that engine (default: the newest). Permanent")
	cmd.Flags().StringArrayVar(&spec.Extensions, "extension", nil,
		"a Postgres extension to create the database with — pgvector, vectorchord. Repeat for more than one. `db extension add` installs more later; nothing removes one. See `db engines`")
	cmd.Flags().StringVar(&spec.Username, "username", "", `the login to create (default "cubeship")`)
	cmd.Flags().StringVar(&spec.Password, "password", "", "the password to set (default: generated)")
	cmd.Flags().StringVar(&spec.Database, "database", "", "the database to create inside the server (default: the name, with underscores)")
	cmd.Flags().StringVar(&spec.Description, "description", "", "what this database is for")
	cmd.Flags().IntVar(&expose, "expose", 0, "publish on a host port; 0 picks one. Off unless the flag is given — see `db expose`")
	return cmd
}

// newDatastoreExtensionCmd is `cubeship db extension`.
func newDatastoreExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extension",
		Short: "Install Postgres extensions on a database",
		Long: "Install Postgres extensions on a database that already exists.\n\n" +
			"Adding only. An extension cannot be removed once it is there,\n" +
			"because a column's type, an index or a default may already need\n" +
			"it — and the image that would tell you is the one being taken\n" +
			"away.\n\n" +
			"`db engines` lists what this daemon offers, and at which\n" +
			"versions.",
	}
	cmd.AddCommand(newDatastoreExtensionAddCmd())
	return cmd
}

func newDatastoreExtensionAddCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Install extensions on a database",
		Long: "Install extensions on a database.\n\n" +
			"The container is replaced to pick the new image up, so the\n" +
			"database is unreachable for a few seconds and comes back as\n" +
			"\"provisioning\". Its data survives: it is a directory on the\n" +
			"host, not something inside the container.\n\n" +
			"Asking for what is already there does nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			d, err := c.AddDatastoreExtensions(context.Background(), args[0], names)
			if err != nil {
				return err
			}
			// What came back, not what was asked for: the daemon adds
			// what an extension requires, and keeps what was already
			// there.
			fmt.Printf("%s now has: %s\n", d.Name, strings.Join(d.Extensions, ", "))
			fmt.Printf("Its container is being replaced. `cubeship db get %s` says when it is back.\n", d.Name)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&names, "extension", nil,
		"an extension to install — pgvector, vectorchord. Repeat for more than one")
	cmd.MarkFlagRequired("extension")
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
			fmt.Fprintln(w, "NAME\tENGINE\tSTATUS\tEXPOSED\tATTACHED")
			for _, d := range all {
				exposed := "-"
				if d.ExposedPort != 0 {
					exposed = fmt.Sprintf("%d", d.ExposedPort)
				}
				fmt.Fprintf(w, "%s\t%s %s\t%s\t%s\t%s\n",
					d.Name, d.Engine, d.Version, d.Status, exposed, attachedList(d))
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
		Use:   "get <name>",
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
			fmt.Printf("Name:        %s\n", d.Name)
			fmt.Printf("Engine:      %s %s\n", d.Engine, d.Version)
			if len(d.Extensions) > 0 {
				fmt.Printf("Extensions:  %s\n", strings.Join(d.Extensions, ", "))
			}
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
			"will keep running.\n\n" +
			"The Postgres extensions this daemon offers are listed under the\n" +
			"table, with the versions each one is available at — an extension\n" +
			"is offered per version rather than per engine.",
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
			if err := w.Flush(); err != nil {
				return err
			}
			// Below the table rather than a column in it: an extension is
			// offered per version, not per engine, and a column would
			// have to either lie about that or not fit.
			for _, e := range engines {
				for _, x := range e.Extensions {
					fmt.Printf("\n%s extension %s (SQL: %s)\n", e.Engine, x.Name, x.SQLName)
					fmt.Printf("  versions: %s\n", strings.Join(x.Versions, ", "))
					if len(x.Requires) > 0 {
						fmt.Printf("  brings:   %s\n", strings.Join(x.Requires, ", "))
					}
					fmt.Printf("  %s\n", x.Summary)
				}
			}
			return nil
		},
	}
}

func newDatastoreCredentialsCmd() *cobra.Command {
	var quiet bool
	cmd := &cobra.Command{
		Use:     "credentials <name>",
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
