package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

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
