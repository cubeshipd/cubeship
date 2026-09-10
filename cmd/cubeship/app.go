package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

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
	var image string
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
			created, err := c.CreateApp(ctx, args[0], project, environment, source, image)
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
	createCmd.Flags().StringVar(&image, "image", "", `for --source external: the image it pulls, without a tag — the tag is a deploy's argument`)

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

	appCmd.AddCommand(createCmd, listCmd, getCmd, deployCmd, deploymentsCmd, deleteCmd, logsCmd,
		newAppPlaceCmd(), newAppLimitsCmd(), newAppAutoscaleCmd(), appEnvCommands())
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

// serversOf renders where an app runs, for a column with room for one
// line.
//
// No machine is marked: traffic arrives at this instance whichever one
// runs the app. An app whose machines disagree about which version to
// serve says so here, because each copy is running *something* and
// without it two versions read as one healthy app.
func serversOf(a client.App) string {
	if len(a.Nodes) == 0 {
		return "-"
	}
	out := strings.Join(a.Nodes, ",")
	if a.Scale > len(a.Nodes) {
		out = fmt.Sprintf("%s (%d copies)", out, a.Scale)
	}
	if a.Split {
		out += " (2 versions)"
	}
	return out
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

// newAppLimitsCmd is `cubeship app limits`.
func newAppLimitsCmd() *cobra.Command {
	var cpu, memory string
	cmd := &cobra.Command{
		Use:   "limits <reference>",
		Short: "Cap what one copy of an app may use",
		Long: "Cap how much of a machine one copy of an app may take.\n\n" +
			"--cpu is cores and may be fractional: 0.5 is half a core. It\n" +
			"is a ceiling rather than a share — a container at its limit is\n" +
			"throttled, not merely preferred less when the machine is busy.\n\n" +
			"--memory takes a size: 512Mi, 2Gi, 1500M. It is enforced by\n" +
			"the kernel killing whatever crosses it, so lowering one below\n" +
			"what a container is already holding kills it on the spot.\n\n" +
			"Both are per copy. An app with three replicas and --cpu 1 may\n" +
			"take three cores between them.\n\n" +
			"Raising or lowering one takes effect immediately, on every\n" +
			"machine the app runs on, without a deploy: a ceiling is the\n" +
			"one part of a container Docker can change while it runs.\n" +
			"Removing one — passing 0 — is the exception, because Docker\n" +
			"reads a zero as \"leave that one alone\": the app goes back to\n" +
			"uncapped on its next deploy.\n\n" +
			"With no flags it prints what the app is capped at now.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if cpu == "" && memory == "" {
				app, err := c.GetApp(context.Background(), args[0])
				if err != nil {
					return err
				}
				fmt.Println(describeLimits(app.Limits))
				return nil
			}

			// Whatever is not being changed is sent as it stands: the
			// ceiling travels whole, so leaving a flag off must not
			// read as removing that half.
			app, err := c.GetApp(context.Background(), args[0])
			if err != nil {
				return err
			}
			limits := app.Limits
			if cpu != "" {
				if limits.CPU, err = strconv.ParseFloat(cpu, 64); err != nil {
					return fmt.Errorf("--cpu takes a number of cores, like 0.5 or 2: %w", err)
				}
			}
			if memory != "" {
				if limits.Memory, err = parseSize(memory); err != nil {
					return err
				}
			}

			capped, err := c.SetAppLimits(context.Background(), args[0], limits)
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s\n", capped.Reference, describeLimits(capped.Limits))
			if capped.Limits.CPU == 0 || capped.Limits.Memory == 0 {
				fmt.Println("A limit removed here takes effect on the next deploy; one changed is already in force.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cpu, "cpu", "", "cores one copy may use, fractional allowed; 0 removes the limit")
	cmd.Flags().StringVar(&memory, "memory", "", "memory one copy may hold, e.g. 512Mi or 2Gi; 0 removes the limit")
	return cmd
}

func describeLimits(l client.Limits) string {
	switch {
	case l.CPU == 0 && l.Memory == 0:
		return "no limits — one copy may take the whole machine"
	case l.Memory == 0:
		return fmt.Sprintf("%g cores per copy, memory uncapped", l.CPU)
	case l.CPU == 0:
		return fmt.Sprintf("%s per copy, CPU uncapped", formatSize(l.Memory))
	default:
		return fmt.Sprintf("%g cores and %s per copy", l.CPU, formatSize(l.Memory))
	}
}

// parseSize reads a size the way everything else that takes one does:
// a number, optionally followed by a unit. A bare number is bytes.
//
// Both spellings are accepted and both mean 1024 — "512M" from somebody
// who thinks in megabytes and "512Mi" from somebody being precise about
// it are the same request, and refusing one of them would be pedantry
// in front of a limit.
func parseSize(in string) (int64, error) {
	s := strings.TrimSpace(in)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		factor int64
	}{
		{"Gi", 1 << 30}, {"G", 1 << 30}, {"Mi", 1 << 20}, {"M", 1 << 20},
		{"Ki", 1 << 10}, {"K", 1 << 10}, {"B", 1},
	} {
		if len(s) > len(u.suffix) && strings.EqualFold(s[len(s)-len(u.suffix):], u.suffix) {
			mult, s = u.factor, s[:len(s)-len(u.suffix)]
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size: give bytes, or a number with Ki, Mi or Gi after it", in)
	}
	return int64(n * float64(mult)), nil
}

// formatSize is parseSize's counterpart, to the nearest unit that leaves
// a short number.
func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%gGi", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%gMi", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%gKi", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// newAppAutoscaleCmd is `cubeship app autoscale`.
func newAppAutoscaleCmd() *cobra.Command {
	var minReplicas, maxReplicas int
	var cpu float64
	var off bool
	cmd := &cobra.Command{
		Use:   "autoscale <reference>",
		Short: "Let this instance decide how many copies to run",
		Long: "Hand an app's replica count to this instance.\n\n" +
			"It reads the average CPU across the app's copies over the\n" +
			"last three minutes — what the app's own chart shows — and\n" +
			"works towards --cpu on each of them. 100 is one core, the\n" +
			"same scale the charts are drawn on, so the number you type\n" +
			"is the number you were looking at.\n\n" +
			"--max is required and is not a formality: without a ceiling\n" +
			"a loop of requests is a loop of replicas until the machine\n" +
			"has nothing left, which is a worse outage than the one this\n" +
			"was turned on to avoid.\n\n" +
			"It is damped and none of that is adjustable: within 10% of\n" +
			"target nothing moves, fewer than three readings is waited\n" +
			"out, and after a change it waits three minutes before the\n" +
			"next — ten before a smaller one, because an extra copy costs\n" +
			"some memory and one copy too few costs the app its latency\n" +
			"exactly as load comes back.\n\n" +
			"CPU is the only signal. Adding a copy does not lower any\n" +
			"copy's memory, so a memory rule would climb and never return.\n\n" +
			"--off hands the count back. With no flags it prints the rule.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			app, err := c.GetApp(context.Background(), args[0])
			if err != nil {
				return err
			}
			changing := off || cmd.Flags().Changed("min") ||
				cmd.Flags().Changed("max") || cmd.Flags().Changed("cpu")
			if !changing {
				fmt.Println(describeAutoscale(app.Autoscale, app.Scale))
				return nil
			}
			if off {
				scaled, err := c.SetAppAutoscale(context.Background(), args[0], client.Autoscale{})
				if err != nil {
					return err
				}
				fmt.Printf("%s: %s It stays at %d cop%s until you say otherwise.\n",
					scaled.Reference, describeAutoscale(scaled.Autoscale, scaled.Scale),
					scaled.Scale, plural(scaled.Scale))
				return nil
			}

			// Whatever is not being changed stays as it is: the rule
			// travels whole, so leaving a flag off must not read as
			// clearing that part of it.
			rule := client.Autoscale{Min: app.Autoscale.Min, Max: app.Autoscale.Max, CPU: app.Autoscale.CPU}
			if cmd.Flags().Changed("min") {
				rule.Min = minReplicas
			}
			if cmd.Flags().Changed("max") {
				rule.Max = maxReplicas
			}
			if cmd.Flags().Changed("cpu") {
				rule.CPU = cpu
			}
			if rule.Min == 0 {
				// The floor nobody thinks about. An app has to run
				// somewhere, so one is the only answer that is not a
				// refusal, and asking for it is a step in front of a
				// button for a value with no alternative.
				rule.Min = 1
			}

			scaled, err := c.SetAppAutoscale(context.Background(), args[0], rule)
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s\n", scaled.Reference, describeAutoscale(scaled.Autoscale, scaled.Scale))
			return nil
		},
	}
	cmd.Flags().IntVar(&minReplicas, "min", 0, "fewest copies to leave running (default 1)")
	cmd.Flags().IntVar(&maxReplicas, "max", 0, "most copies to run — required, and there is no unlimited")
	cmd.Flags().Float64Var(&cpu, "cpu", 0, "CPU each copy should sit at, where 100 is one core")
	cmd.Flags().BoolVar(&off, "off", false, "hand the count back: the app stays where it is")
	return cmd
}

func describeAutoscale(a client.Autoscale, running int) string {
	if !a.On() {
		return fmt.Sprintf("not autoscaled — it runs the %d cop%s you asked for", running, plural(running))
	}
	out := fmt.Sprintf("%d to %d copies, each aiming at %g%% of a core", a.Min, a.Max, a.CPU)
	if a.At != nil {
		out += fmt.Sprintf(" (last changed %s ago)", time.Since(*a.At).Round(time.Minute))
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
