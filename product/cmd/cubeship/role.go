package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newRoleCmd is `cubeship role`: the access roles members and API keys
// are given. Composing one is a screen's job — Users → Roles — so this
// reads them.
func newRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "role",
		Aliases: []string{"roles"},
		Short:   "Read the access roles members and API keys are given",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the access roles and what each one grants",
		Long: "List the access roles on this instance. Each grant is a kind of\n" +
			"resource, a level (view or manage), whether it reads secrets —\n" +
			"variables, credentials, files — and which items, or every one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			roles, err := c.ListRoles(context.Background())
			if err != nil {
				return err
			}
			if len(roles) == 0 {
				fmt.Println("No roles.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ROLE\tMEMBERS\tKEYS\tGRANTS")
			for _, r := range roles {
				var grants []string
				for _, g := range r.Grants {
					if g.Level == "none" {
						continue
					}
					part := g.Resource + ":" + g.Level
					if g.Secrets {
						part += "+secrets"
					}
					if g.Items != nil {
						part += "[" + strings.Join(g.Items, ",") + "]"
					}
					grants = append(grants, part)
				}
				fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", r.Name, r.Members, r.Keys, dash(strings.Join(grants, " ")))
			}
			return w.Flush()
		},
	})
	return cmd
}
