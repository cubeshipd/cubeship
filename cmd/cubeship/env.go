package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

// envCommands builds the "env" command group for one level — project,
// environment or app. All three take a single positional argument naming
// the resource, and differ only in which client calls they make.
//
// "set" merges and "replace" is a separate, explicitly named command.
// The other way round is how configuration gets destroyed by accident:
// `env set A=1` on an app that already had B used to leave only A, with
// nothing to warn you and no way to look before or after.
type envCommands struct {
	// noun names the level in help text, e.g. "app".
	noun string
	// arg is the positional argument's placeholder, e.g. "<name>".
	arg string

	read    func(c *client.Client, scope string) (client.EnvVars, error)
	merge   func(c *client.Client, scope string, set map[string]string, unset []string) error
	replace func(c *client.Client, scope string, vars map[string]string) error
}

func (e envCommands) command() *cobra.Command {
	envCmd := &cobra.Command{
		Use:   "env",
		Short: fmt.Sprintf("Read and change the %s's environment variables", e.noun),
	}

	listCmd := &cobra.Command{
		Use:   "list " + e.arg,
		Short: fmt.Sprintf("Show the %s's environment variables", e.noun),
		Long: fmt.Sprintf("Show the variables set on this %s.\n\n", e.noun) +
			"Where the level inherits from another, the effective set is shown\n" +
			"instead, with the level each value came from.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			vars, err := e.read(c, args[0])
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			defer w.Flush()

			if len(vars.Effective) > 0 {
				fmt.Fprintln(w, "KEY\tVALUE\tFROM")
				for _, v := range vars.Effective {
					fmt.Fprintf(w, "%s\t%s\t%s\n", v.Key, v.Value, v.Source)
				}
				return nil
			}
			fmt.Fprintln(w, "KEY\tVALUE")
			for _, k := range slices.Sorted(maps.Keys(vars.Vars)) {
				fmt.Fprintf(w, "%s\t%s\n", k, vars.Vars[k])
			}
			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set " + e.arg + " KEY=VALUE [KEY=VALUE...]",
		Short: fmt.Sprintf("Add or change %s variables, leaving the rest alone", e.noun),
		Long: "Add or change variables. Anything you don't name keeps the value\n" +
			"it has — use \"env unset\" to remove one, or \"env replace\" to\n" +
			"overwrite the whole set.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := parseEnvPairs(args[1:])
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := e.merge(c, args[0], vars, nil); err != nil {
				return err
			}
			fmt.Printf("Set %d variable(s) on %s. Redeploy for the change to reach the container.\n", len(vars), args[0])
			return nil
		},
	}

	unsetCmd := &cobra.Command{
		Use:   "unset " + e.arg + " KEY [KEY...]",
		Short: fmt.Sprintf("Remove %s variables, leaving the rest alone", e.noun),
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := args[1:]
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := e.merge(c, args[0], nil, keys); err != nil {
				return err
			}
			fmt.Printf("Removed %d variable(s) from %s. Redeploy for the change to reach the container.\n", len(keys), args[0])
			return nil
		},
	}

	var confirmed bool
	replaceCmd := &cobra.Command{
		Use:   "replace " + e.arg + " [KEY=VALUE...]",
		Short: fmt.Sprintf("Replace every %s variable with exactly these", e.noun),
		Long: "Replace the whole set of variables at this level.\n\n" +
			"Anything not listed is DELETED. With no pairs at all, every\n" +
			"variable at this level is removed. Requires --yes, because this\n" +
			"is the command that can lose configuration.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("this deletes every variable you don't list; pass --yes if that is what you mean")
			}
			vars, err := parseEnvPairs(args[1:])
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := e.replace(c, args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Replaced %s's variables with %d entr(ies).\n", args[0], len(vars))
			return nil
		},
	}
	replaceCmd.Flags().BoolVar(&confirmed, "yes", false, "confirm that variables you don't list should be deleted")

	envCmd.AddCommand(listCmd, setCmd, unsetCmd, replaceCmd)
	return envCmd
}

// parseEnvPairs turns KEY=VALUE arguments into a map.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	vars := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", pair)
		}
		vars[key] = value
	}
	return vars, nil
}

// The three levels, each wired to its own client calls. The scope beyond
// the positional argument — which organization, which project — comes
// from the flags the parent command already declares.

func appEnvCommands() *cobra.Command {
	return envCommands{
		noun: "app",
		arg:  "<name>",
		read: func(c *client.Client, name string) (client.EnvVars, error) {
			return c.AppEnv(context.Background(), name)
		},
		merge: func(c *client.Client, name string, set map[string]string, unset []string) error {
			return c.MergeAppEnv(context.Background(), name, set, unset)
		},
		replace: func(c *client.Client, name string, vars map[string]string) error {
			return c.SetAppEnv(context.Background(), name, vars)
		},
	}.command()
}

func projectEnvCommands() *cobra.Command {
	org := new(string)
	cmd := envCommands{
		noun: "project",
		arg:  "<project-slug>",
		read: func(c *client.Client, slug string) (client.EnvVars, error) {
			return c.ProjectEnv(context.Background(), *org, slug)
		},
		merge: func(c *client.Client, slug string, set map[string]string, unset []string) error {
			return c.MergeProjectEnv(context.Background(), *org, slug, set, unset)
		},
		replace: func(c *client.Client, slug string, vars map[string]string) error {
			return c.SetProjectEnv(context.Background(), *org, slug, vars)
		},
	}.command()

	// A persistent flag on the group, so every subcommand inherits it
	// rather than each declaring its own copy.
	cmd.PersistentFlags().StringVar(org, "org", "", "organization slug")
	cmd.MarkPersistentFlagRequired("org")
	return cmd
}

func environmentEnvCommands() *cobra.Command {
	org, project := new(string), new(string)
	cmd := envCommands{
		noun: "environment",
		arg:  "<env-slug>",
		read: func(c *client.Client, slug string) (client.EnvVars, error) {
			return c.EnvironmentEnv(context.Background(), *org, *project, slug)
		},
		merge: func(c *client.Client, slug string, set map[string]string, unset []string) error {
			return c.MergeEnvironmentEnv(context.Background(), *org, *project, slug, set, unset)
		},
		replace: func(c *client.Client, slug string, vars map[string]string) error {
			return c.SetEnvironmentEnv(context.Background(), *org, *project, slug, vars)
		},
	}.command()

	cmd.PersistentFlags().StringVar(org, "org", "", "organization slug")
	cmd.MarkPersistentFlagRequired("org")
	cmd.PersistentFlags().StringVar(project, "project", "", "project slug")
	cmd.MarkPersistentFlagRequired("project")
	return cmd
}
