package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"cubeship/internal/cli/client"
	"cubeship/internal/cli/creds"

	"github.com/spf13/cobra"
)

func newAPIClient() (*client.Client, error) {
	path, err := creds.DefaultPath()
	if err != nil {
		return nil, err
	}
	saved, err := creds.Load(path)
	if err != nil {
		return nil, err
	}
	return client.New(saved.BaseURL, saved.Token), nil
}

func newAppCmd() *cobra.Command {
	appCmd := &cobra.Command{
		Use:   "app",
		Short: "Manage Cubeship apps",
		Long: "Manage Cubeship apps.\n\n" +
			"An app is named by its reference: org/project/environment/app.\n" +
			"Three parts — org/project/app — means the production environment.\n" +
			"App names only have to be unique inside their environment, so the\n" +
			"same name can exist in production and staging at once.",
	}

	var domain, project, environment, source string
	var port int
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Register a new app and get its registry image path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			ctx := context.Background()
			created, err := c.CreateApp(ctx, args[0], project, environment, source)
			if err != nil {
				return err
			}
			// The domain is a separate call because an app can have
			// several, each naming its own port. Given here it is the
			// first one, which is what most apps ever have.
			if domain != "" {
				if _, err := c.AddAppDomain(ctx, created.Reference, domain, port); err != nil {
					return fmt.Errorf("the app was created but %s could not be added to it: %w", domain, err)
				}
			}
			fmt.Printf("Created %s. Push to: %s\n", created.Reference, created.Image)
			return nil
		},
	}
	createCmd.Flags().StringVar(&domain, "domain", "", "a domain to serve the app on; add more with `app domain add`")
	createCmd.Flags().IntVar(&port, "port", 0, "what that domain reaches inside the container; 0 reads it from the image")
	createCmd.Flags().StringVar(&project, "project", "", "project slug this app belongs to")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().StringVar(&environment, "env", "", `environment slug within the project (default "production")`)
	createCmd.Flags().StringVar(&source, "source", "", `where the image comes from: "registry" (the default) means one you push to Cubeship`)

	var tag string
	var detach bool
	deployCmd := &cobra.Command{
		Use:   "deploy <app>",
		Short: "Manually redeploy an app from the given (or latest) image tag",
		Long: "Redeploy an app from a tag already pushed to its registry path.\n\n" +
			"The deploy runs on the daemon, not in this command — pressing\n" +
			"Ctrl-C, or losing the connection, stops the waiting, not the\n" +
			"deploy. Use \"app deployments\" to catch up on one you stopped\n" +
			"watching.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			deployment, err := c.Deploy(context.Background(), args[0], tag)
			if err != nil {
				return err
			}
			if detach {
				fmt.Printf("Deploy %d of %s started. Check it with: cubeship app deployments %s\n",
					deployment.ID, args[0], args[0])
				return nil
			}

			fmt.Printf("Deploying %s from tag %s...\n", args[0], tag)
			finished, err := c.WaitForDeployment(context.Background(), args[0], deployment.ID)
			if err != nil {
				return err
			}
			switch finished.Status {
			case client.DeploymentSucceeded:
				fmt.Printf("Deployed %s\n", args[0])
				return nil
			case client.DeploymentFailed:
				return fmt.Errorf("deploy failed: %s", finished.Error)
			default:
				fmt.Printf("Deploy %d is still running. Check it with: cubeship app deployments %s\n",
					finished.ID, args[0])
				return nil
			}
		},
	}
	deployCmd.Flags().StringVar(&tag, "tag", "latest", "image tag to deploy")
	deployCmd.Flags().BoolVar(&detach, "detach", false, "start the deploy and return without waiting for it")

	deploymentsCmd := &cobra.Command{
		Use:   "deployments <app>",
		Short: "Show an app's recent deploys and how each one went",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			history, err := c.Deployments(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tWHEN\tIMAGE\tERROR")
			for _, d := range history {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
					d.ID, d.Status, d.CreatedAt.Format("2006-01-02 15:04"), d.Image, d.Error)
			}
			return w.Flush()
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the apps you can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			apps, err := c.ListApps(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "APP\tSTATUS\tDOMAIN")
			for _, a := range apps {
				fmt.Fprintf(w, "%s\t%s\t%s\n", a.Reference, a.Status, hostsOf(a))
			}
			return w.Flush()
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <app>",
		Short: "Show one app, including its registry push path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			a, err := c.GetApp(context.Background(), args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, row := range [][2]string{
				{"App", a.Reference}, {"Project", a.Project},
				{"Environment", a.Environment}, {"Domains", hostsOf(a)},
				{"Source", a.Source}, {"Status", a.Status}, {"Push to", a.Image},
			} {
				fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
			}
			return w.Flush()
		},
	}

	var tail string
	logsCmd := &cobra.Command{
		Use:   "logs <app>",
		Short: "Print an app's recent container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rc, err := c.Logs(context.Background(), args[0], tail)
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}
	logsCmd.Flags().StringVar(&tail, "tail", "", `number of trailing lines, or "all" (default: the daemon's own limit)`)

	var deleteConfirmed bool
	deleteCmd := &cobra.Command{
		Use:   "delete <app>",
		Short: "Delete an app and stop the container serving it",
		Long: "Delete an app.\n\n" +
			"The container serving it is stopped and removed first. Images you\n" +
			"already pushed stay in the registry. This cannot be undone.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deleteConfirmed {
				return fmt.Errorf("this stops %s and deletes it for good; pass --yes to confirm", args[0])
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.DeleteApp(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&deleteConfirmed, "yes", false, "confirm that the app should be deleted")

	appCmd.AddCommand(createCmd, listCmd, getCmd, deployCmd, deploymentsCmd, deleteCmd, logsCmd, appEnvCommands())
	return appCmd
}

// hostsOf renders every name an app answers at, for a column that has
// room for one line. An app with several is common now: one image can
// expose more than one port, and each name says which it reaches.
func hostsOf(a client.App) string {
	if len(a.Domains) == 0 {
		return ""
	}
	hosts := make([]string, 0, len(a.Domains))
	for _, d := range a.Domains {
		hosts = append(hosts, d.Host)
	}
	return strings.Join(hosts, ", ")
}
