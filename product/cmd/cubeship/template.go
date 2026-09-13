package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

func newTemplateCmd() *cobra.Command {
	templateCmd := &cobra.Command{Use: "template", Short: "Install apps from the template catalog"}

	var tag string
	listCmd := &cobra.Command{
		Use:   "list [search]",
		Short: "Search the template catalog",
		Long:  "Search the template catalog at cubeship.dev. The search matches a\ntemplate's name, description or tags.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			q := ""
			if len(args) == 1 {
				q = args[0]
			}
			page, err := c.ListTemplates(context.Background(), q, tag)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TEMPLATE\tRELEASE\tSTARS\tDESCRIPTION")
			for _, t := range page.Templates {
				fmt.Fprintf(w, "%s/%s\t%s\t%d\t%s\n", t.Owner, t.Name, t.Release.Tag, t.Stars, t.Description)
			}
			return w.Flush()
		},
	}
	listCmd.Flags().StringVar(&tag, "tag", "", "only templates with this tag")

	var req client.InstallTemplateRequest
	var inputs, databases, stores, apps []string
	installCmd := &cobra.Command{
		Use:   "install <owner/repo>",
		Short: "Install a template and wait for it to finish",
		Long: "Install a template from the catalog: its project and environment when\n" +
			"they do not exist, its databases, stores and apps, and a deploy of\n" +
			"each app. Anything that fails is undone.\n\n" +
			"Secrets the instance generates are printed once, here, and nowhere else.",
		Example: "  cubeship template install cubeshipd/cubeship-umami-template --input domain=analytics.example.com",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo, ok := strings.Cut(args[0], "/")
			if !ok || owner == "" || repo == "" {
				return errors.New("name the template as owner/repo, as `cubeship template list` shows it")
			}
			var err error
			if req.Inputs, err = keyValues("--input", inputs); err != nil {
				return err
			}
			if req.Names.Databases, err = keyValues("--database-name", databases); err != nil {
				return err
			}
			if req.Names.Stores, err = keyValues("--store-name", stores); err != nil {
				return err
			}
			if req.Names.Apps, err = keyValues("--app-name", apps); err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			started, err := c.InstallTemplate(context.Background(), owner, repo, req)
			if err != nil {
				return err
			}
			fmt.Printf("Installing %s %s into %s/%s\n", args[0], started.Install.Release,
				started.Install.Project, started.Install.Environment)
			printSecrets(started.Secrets)
			return followRun(c, started.Install.ID)
		},
	}
	installCmd.Flags().StringVar(&req.Release, "release", "", "a release tag; the newest the catalog accepted by default")
	installCmd.Flags().StringVar(&req.Project, "project", "", "the project to install into; the template's suggestion by default")
	installCmd.Flags().StringVar(&req.Environment, "env", "", "the environment inside it; the template's by default")
	installCmd.Flags().StringArrayVar(&inputs, "input", nil, "an answer to one of the template's inputs, key=value (repeatable)")
	installCmd.Flags().StringArrayVar(&databases, "database-name", nil, "rename a database the template declares, key=name (repeatable)")
	installCmd.Flags().StringArrayVar(&stores, "store-name", nil, "rename an object store the template declares, key=name (repeatable)")
	installCmd.Flags().StringArrayVar(&apps, "app-name", nil, "rename an app the template declares, key=name (repeatable)")

	installedCmd := &cobra.Command{
		Use:   "installed",
		Short: "List the templates installed on this instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			installs, err := c.ListTemplateInstalls(context.Background())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTEMPLATE\tINTO\tRELEASE\tSTATUS")
			for _, in := range installs {
				release := in.Release
				if in.UpdateAvailable != nil {
					release += " (" + *in.UpdateAvailable + " available)"
				}
				status := in.Status
				if in.Busy && len(in.Runs) > 0 {
					status = in.Runs[0].Kind + " running"
				}
				fmt.Fprintf(w, "%d\t%s/%s\t%s/%s\t%s\t%s\n", in.ID, in.Owner, in.Repo, in.Project, in.Environment, release, status)
			}
			return w.Flush()
		},
	}

	var updateRelease string
	var updateInputs []string
	var updateConfirmed bool
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an installation to a newer release",
		Long: "Show what updating an installation would change, and with --yes apply it.\n" +
			"An update creates and changes what the release does and deletes nothing;\n" +
			"if a step fails, the installation is put back as it was.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not an installation id; `cubeship template installed` lists them", args[0])
			}
			inputs, err := keyValues("--input", updateInputs)
			if err != nil {
				return err
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			preview, err := c.PreviewTemplateUpdate(context.Background(), id, updateRelease)
			if err != nil {
				return err
			}
			if preview.From == preview.To {
				fmt.Printf("Already on %s.\n", preview.To)
				return nil
			}
			fmt.Printf("Updating %s → %s would:\n", preview.From, preview.To)
			for _, ch := range preview.Changes {
				line := fmt.Sprintf("  %-6s %s %s", ch.Action, ch.Kind, ch.Name)
				if ch.Detail != "" {
					line += " — " + ch.Detail
				}
				fmt.Println(line)
			}
			var missing []string
			for _, in := range preview.Inputs {
				if _, answered := inputs[in.Key]; !answered {
					missing = append(missing, fmt.Sprintf("--input %s=… (%s)", in.Key, in.Label))
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("the release asks questions this installation has no answer for; pass %s", strings.Join(missing, ", "))
			}
			if !updateConfirmed {
				fmt.Println("\nPass --yes to apply it.")
				return nil
			}
			started, err := c.UpdateTemplateInstall(context.Background(), id, preview.To, inputs)
			if err != nil {
				return err
			}
			printSecrets(started.Secrets)
			return followRun(c, id)
		},
	}
	updateCmd.Flags().StringVar(&updateRelease, "release", "", "a release tag; the newest the catalog accepted by default")
	updateCmd.Flags().StringArrayVar(&updateInputs, "input", nil, "an answer to a question the release adds, key=value (repeatable)")
	updateCmd.Flags().BoolVar(&updateConfirmed, "yes", false, "apply the update rather than only showing it")

	var deleteData, uninstallConfirmed bool
	uninstallCmd := &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Uninstall an installation",
		Long: "Delete an installation's apps, and the project and environment it created\n" +
			"once they are empty. Its databases and object stores are kept unless\n" +
			"--delete-data, which deletes them and everything in them for good.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not an installation id; `cubeship template installed` lists them", args[0])
			}
			if !uninstallConfirmed {
				what := "its apps"
				if deleteData {
					what = "its apps, databases and object stores, and their data,"
				}
				return fmt.Errorf("this deletes %s for good; pass --yes to confirm", what)
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if _, err := c.UninstallTemplateInstall(context.Background(), id, !deleteData); err != nil {
				return err
			}
			return followRun(c, id)
		},
	}
	uninstallCmd.Flags().BoolVar(&deleteData, "delete-data", false, "also delete its databases and object stores, and their data")
	uninstallCmd.Flags().BoolVar(&uninstallConfirmed, "yes", false, "confirm the uninstall")

	releasesCmd := &cobra.Command{
		Use:   "releases <owner/repo>",
		Short: "List the versions a template can be installed at",
		Long: "List the releases of a template the catalog accepted, newest first.\n" +
			"Install one with `cubeship template install --release <tag>`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo, ok := strings.Cut(args[0], "/")
			if !ok || owner == "" || repo == "" {
				return errors.New("name the template as owner/repo, as `cubeship template list` shows it")
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			releases, err := c.ListTemplateReleases(context.Background(), owner, repo)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "RELEASE\tPUBLISHED")
			for _, r := range releases {
				fmt.Fprintf(w, "%s\t%s\n", r.Tag, r.PublishedAt)
			}
			return w.Flush()
		},
	}

	templateCmd.AddCommand(listCmd, releasesCmd, installCmd, installedCmd, updateCmd, uninstallCmd)
	return templateCmd
}

// followRun prints each step of an installation's newest run once and
// returns when that run ends.
func followRun(c *client.Client, id int64) error {
	last := ""
	for {
		in, err := c.GetTemplateInstall(context.Background(), id)
		if err != nil {
			return err
		}
		if len(in.Runs) == 0 {
			return errors.New("the installation has no run to follow")
		}
		run := in.Runs[0]
		if run.Step != "" && run.Step != last {
			fmt.Println("  " + run.Step)
			last = run.Step
		}
		switch run.Status {
		case "succeeded":
			fmt.Printf("Done: %s is %s", in.Owner+"/"+in.Repo, in.Status)
			if in.Status != "uninstalled" {
				fmt.Printf(" on %s", in.Release)
			}
			fmt.Println(".")
			for _, r := range run.Created {
				fmt.Printf("  created %s %s\n", r.Kind, r.Name)
			}
			return nil
		case "failed":
			return fmt.Errorf("the %s failed and was undone: %s", run.Kind, run.Error)
		}
		time.Sleep(2 * time.Second)
	}
}

func printSecrets(secrets map[string]string) {
	if len(secrets) == 0 {
		return
	}
	keys := make([]string, 0, len(secrets))
	for key := range secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Println("\nGenerated secrets — shown this once, keep a copy:")
	for _, key := range keys {
		fmt.Printf("  %s: %s\n", key, secrets[key])
	}
	fmt.Println()
}

func keyValues(flag string, pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%s takes key=value, not %q", flag, pair)
		}
		out[key] = value
	}
	return out, nil
}
