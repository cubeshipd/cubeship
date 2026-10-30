package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
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
			"An app is named by its reference: project/environment/app.\n" +
			"Two parts — project/app — means the production environment.\n" +
			"App names only have to be unique inside their environment, so the\n" +
			"same name can exist in production and staging at once.",
	}

	var domain, project, environment, source string
	var port int
	var image, createTag string
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
			created, err := c.CreateApp(ctx, args[0], project, environment, source, image, createTag)
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
	createCmd.Flags().StringVar(&image, "image", "", `for --source external: the image it pulls, without a tag — the tag is its own flag`)
	createCmd.Flags().StringVar(&createTag, "tag", "", `the tag to run; leave it out to follow the registry, which on Cubeship's own means a push deploys the app`)

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

			// The deploy row carries the tag that was actually
			// resolved, which is the only place it is known when none
			// was asked for.
			if deployment.Image != "" {
				fmt.Printf("Deploying %s from tag %s...\n", args[0], deployment.Image)
			} else {
				fmt.Printf("Deploying %s...\n", args[0])
			}
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
	// No default. "latest" here was sent as if somebody had asked for
	// it, which is not the same request as asking for nothing: the
	// daemon answers the empty one with the app's own tag — what it is
	// pinned to, or what it is already running.
	deployCmd.Flags().StringVar(&tag, "tag", "", "image tag to deploy (default: the tag the app is on)")
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
			fmt.Fprintln(w, "APP\tSTATUS\tSERVERS\tDOMAIN")
			for _, a := range apps {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Reference, a.Status, serversOf(a), hostsOf(a))
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
				{"Internal host", a.InternalHost},
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

	var shell string
	var timeout int64
	predeployCmd := &cobra.Command{
		Use:   "predeploy <app> [--] <command> [args...]",
		Short: "Set the command run from the new image before a deploy swap",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			var command any = args[1:]
			if shell != "" {
				command = shell
			}
			_, err = c.SetAppPredeploy(context.Background(), args[0], command, timeout)
			return err
		},
	}
	predeployCmd.Flags().StringVar(&shell, "shell", "", "run this shell command instead of argv")
	predeployCmd.Flags().Int64Var(&timeout, "timeout", 0, "timeout in seconds (maximum 3600; default 600)")
	cancelCmd := &cobra.Command{
		Use: "cancel <app> <deployment-id>", Short: "Cancel a running deployment", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid deployment id: %w", err)
			}
			if err := c.CancelDeployment(context.Background(), args[0], id); err != nil {
				return err
			}
			fmt.Printf("Cancelled deploy %d of %s\n", id, args[0])
			return nil
		},
	}
	rollbackCmd := &cobra.Command{
		Use:   "rollback <app> <deployment-id>",
		Short: "Deploy the version an earlier deployment ran",
		Long: "Deploy again the image tag, or the Git commit, of an earlier\n" +
			"deployment that succeeded. \"app deployments\" lists the ids.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid deployment id: %w", err)
			}
			deployment, err := c.Rollback(context.Background(), args[0], id)
			if err != nil {
				return err
			}
			fmt.Printf("Rolling %s back to deploy %d...\n", args[0], id)
			finished, err := c.WaitForDeployment(context.Background(), args[0], deployment.ID)
			if err != nil {
				return err
			}
			switch finished.Status {
			case client.DeploymentSucceeded:
				fmt.Printf("Rolled %s back to deploy %d\n", args[0], id)
				return nil
			case client.DeploymentFailed:
				return fmt.Errorf("rollback failed: %s", finished.Error)
			default:
				fmt.Printf("Deploy %d is still running. Check it with: cubeship app deployments %s\n",
					finished.ID, args[0])
				return nil
			}
		},
	}

	appCmd.AddCommand(createCmd, listCmd, getCmd, deployCmd, deploymentsCmd, deleteCmd, logsCmd, newAppShellCmd(),
		newAppPlaceCmd(), newAppLimitsCmd(), newAppAutoscaleCmd(), predeployCmd, cancelCmd, rollbackCmd, appEnvCommands(), newAppVolumeCmd(), newAppTCPCmd())
	return appCmd
}

// newAppPlaceCmd is `cubeship app place`.
//
// Where an app runs and where its traffic arrives are two decisions, so
// they are two flags. Scaling out is adding a machine to --on; moving
// where the DNS record points is --edge. Conflating them would mean a
// name that moves every time a replica is added.
func newAppPlaceCmd() *cobra.Command {
	var on []string
	var replicas int
	var everywhere bool
	cmd := &cobra.Command{
		Use:   "place <reference>",
		Short: "Choose which machines run an app, and how many copies",
		Long: "Choose which machines run an app, and how many copies of it.\n\n" +
			"More than one machine puts this instance's proxy in front of\n" +
			"every copy, round-robin, over the cluster's private network.\n" +
			"Each new machine starts the app before the ones leaving stop\n" +
			"it, so a placement that fails is not an outage.\n\n" +
			"--replicas is how many copies run in total, spread over those\n" +
			"machines round-robin: four over three is 2, 1, 1. Never fewer\n" +
			"than there are machines — a machine given nothing to run is a\n" +
			"machine placed there for no effect. On one machine, several\n" +
			"copies are swapped one at a time, so a deploy is a rolling one\n" +
			"rather than a moment with none of them serving.\n\n" +
			"--everywhere makes the app follow the cluster: it runs on\n" +
			"every machine there is, and on any that joins later. It is a\n" +
			"switch rather than a third way of naming machines — --on and\n" +
			"--replicas already say where and how many, and what they\n" +
			"cannot say is \"wherever the cluster goes\". Passing --on\n" +
			"turns it off again, because that is choosing by hand.\n\n" +
			"Nothing about this touches DNS. Every name this instance\n" +
			"serves arrives at the control plane, which routes it to\n" +
			"whichever machine runs the app — so a record points here once\n" +
			"and never moves again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var spread *bool
			if cmd.Flags().Changed("everywhere") {
				spread = &everywhere
			}
			if len(on) == 0 && replicas == 0 && spread == nil {
				return fmt.Errorf("nothing to change: pass --on, --replicas or --everywhere")
			}
			// Two answers to "which machines". The daemon would take
			// the named ones and turn the switch off, which is the
			// right reading of `--on` alone and a silent one of both.
			if len(on) > 0 && everywhere {
				return fmt.Errorf("--on and --everywhere are two answers to which machines: --everywhere is every machine there is, and naming some is choosing by hand")
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			placed, err := c.PlaceApp(context.Background(), args[0], on, replicas, spread)
			if err != nil {
				return err
			}
			copies := "1 copy"
			if placed.Scale != 1 {
				copies = fmt.Sprintf("%d copies", placed.Scale)
			}
			where := strings.Join(placed.Nodes, ", ")
			if placed.Spread {
				where += " — every machine in the cluster, and any that joins"
			}
			fmt.Printf("%s runs %s on %s.\n", placed.Reference, copies, where)
			if placed.Address != "" {
				fmt.Printf("Its traffic arrives at this instance, %s, whichever machine runs it.\n", placed.Address)
			}
			// Said once, where the decision was just made: the record
			// does not move when an app does, which is the whole point
			// of one front door.
			if len(placed.Domains) > 0 && placed.Address != "" {
				fmt.Printf("%s already points here — moving an app does not change that.\n",
					hostsOf(placed))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&on, "on", nil, "the machines that run it, by name")
	cmd.Flags().IntVar(&replicas, "replicas", 0, "how many copies to run in total, spread over those machines (default: unchanged)")
	cmd.Flags().BoolVar(&everywhere, "everywhere", false, "follow the cluster: run on every machine there is, and on any that joins")
	return cmd
}
