package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
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
			if len(started.Secrets) > 0 {
				keys := make([]string, 0, len(started.Secrets))
				for key := range started.Secrets {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				fmt.Println("\nGenerated secrets — shown this once, keep a copy:")
				for _, key := range keys {
					fmt.Printf("  %s: %s\n", key, started.Secrets[key])
				}
				fmt.Println()
			}
			return followInstall(c, started.Install)
		},
	}
	installCmd.Flags().StringVar(&req.Release, "release", "", "a release tag; the newest the catalog accepted by default")
	installCmd.Flags().StringVar(&req.Project, "project", "", "the project to install into; the template's suggestion by default")
	installCmd.Flags().StringVar(&req.Environment, "env", "", "the environment inside it; the template's by default")
	installCmd.Flags().StringArrayVar(&inputs, "input", nil, "an answer to one of the template's inputs, key=value (repeatable)")
	installCmd.Flags().StringArrayVar(&databases, "database-name", nil, "rename a database the template declares, key=name (repeatable)")
	installCmd.Flags().StringArrayVar(&stores, "store-name", nil, "rename an object store the template declares, key=name (repeatable)")
	installCmd.Flags().StringArrayVar(&apps, "app-name", nil, "rename an app the template declares, key=name (repeatable)")

	templateCmd.AddCommand(listCmd, installCmd)
	return templateCmd
}

// followInstall prints each step once and returns when the install ends.
func followInstall(c *client.Client, in client.TemplateInstall) error {
	last := ""
	for {
		if in.Step != "" && in.Step != last {
			fmt.Println("  " + in.Step)
			last = in.Step
		}
		switch in.Status {
		case "succeeded":
			fmt.Println("Installed. Created:")
			for _, r := range in.Resources {
				fmt.Printf("  %s %s\n", r.Kind, r.Name)
			}
			return nil
		case "failed":
			return fmt.Errorf("the install failed and was undone: %s", in.Error)
		}
		time.Sleep(2 * time.Second)
		next, err := c.GetTemplateInstall(context.Background(), in.ID)
		if err != nil {
			return err
		}
		in = next
	}
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
