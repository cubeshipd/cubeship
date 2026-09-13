package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"cubeship/internal/cli/client"

	"github.com/spf13/cobra"
)

// newAuditCmd is `cubeship audit`.
func newAuditCmd() *cobra.Command {
	var f client.AuditFilter
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Read who changed what on this instance",
		Long: "Read the audit log, newest first: every change made through the\n" +
			"dashboard, the API or MCP, and every refused attempt — a read\n" +
			"included, so a key trying to read a secret shows up. Request\n" +
			"bodies are never kept, and events are kept for 90 days.\n\n" +
			"Admin only. The last line says how to read the page before it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			page, err := c.ListAudit(context.Background(), f)
			if err != nil {
				return err
			}
			if len(page.Events) == 0 {
				fmt.Println("No events.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tWHEN\tWHO\tVIA\tWHAT\tOUTCOME")
			for _, e := range page.Events {
				who := e.Username
				if e.KeyName != "" {
					who += " (" + e.KeyName + ")"
				}
				outcome := e.Outcome
				if e.Detail != "" {
					outcome += ": " + e.Detail
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
					e.ID, e.At.Local().Format("2006-01-02 15:04:05"), who, e.Via, e.Summary, outcome)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if page.Next > 0 {
				fmt.Printf("\nOlder: cubeship audit --before %d\n", page.Next)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.User, "user", "", "only this username")
	cmd.Flags().StringVar(&f.Via, "via", "", "dashboard, api or mcp")
	cmd.Flags().StringVar(&f.Outcome, "outcome", "", "ok, refused or failed")
	cmd.Flags().StringVar(&f.Target, "target", "", "only events whose target contains this, e.g. an app's reference")
	cmd.Flags().StringVar(&f.From, "from", "", "only events at or after this: YYYY-MM-DD (midnight UTC) or RFC 3339")
	cmd.Flags().StringVar(&f.To, "to", "", "only events before this, in the same form")
	cmd.Flags().Int64Var(&f.Before, "before", 0, "only events older than this id")
	cmd.Flags().IntVar(&f.Limit, "limit", 0, "at most this many, up to 500 (default 100)")
	return cmd
}
