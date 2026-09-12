package main

// `cubeship docs <dir>` writes the CLI's reference into the site's docs:
// one MDX page per top-level command, every subcommand under it, every
// flag — from the same cobra tree `--help` prints. Generated so the
// reference *is* the CLI rather than a description of it, and `--check`
// fails when the pages on disk are not what this would write, which is
// what CI runs: a flag added without the page following it is the page
// going stale.
//
// Hidden, because it is a build step (`make reference`), not something
// an operator types.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const generatedNotice = "{/* Generated from the CLI by `cubeship docs` — run `make reference`. " +
	"Editing this file is editing the copy rather than the thing. */}\n\n"

func newDocsCmd(root *cobra.Command) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:    "docs <dir>",
		Short:  "Write the CLI reference as MDX pages",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return writePages(args[0], referencePages(root), check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "fail if the pages on disk are not what this would write")
	return cmd
}

// referencePages is one page per top-level command, keyed by file name.
func referencePages(root *cobra.Command) map[string]string {
	pages := map[string]string{}
	for _, cmd := range root.Commands() {
		if !cmd.IsAvailableCommand() || cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		pages[cmd.Name()+".mdx"] = renderPage(cmd)
	}
	return pages
}

func renderPage(top *cobra.Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %s\ndescription: %s\n---\n\n", top.CommandPath(), strconv.Quote(top.Short))
	b.WriteString(generatedNotice)
	b.WriteString(prose(top) + "\n\n")
	if top.Runnable() {
		renderUsage(&b, top)
	}
	for _, sub := range top.Commands() {
		if sub.IsAvailableCommand() {
			renderCommand(&b, sub)
		}
	}
	return b.String()
}

// renderCommand writes a subcommand, and the subcommands under it: `user
// api-key create` is a heading of its own under `user`.
func renderCommand(b *strings.Builder, cmd *cobra.Command) {
	fmt.Fprintf(b, "## %s\n\n", cmd.CommandPath())
	b.WriteString(prose(cmd) + "\n\n")
	if cmd.Runnable() {
		renderUsage(b, cmd)
	}
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			renderCommand(b, sub)
		}
	}
}

func renderUsage(b *strings.Builder, cmd *cobra.Command) {
	fmt.Fprintf(b, "```bash\n%s\n```\n\n", cmd.UseLine())
	var flags []*pflag.Flag
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			flags = append(flags, f)
		}
	})
	if len(flags) == 0 {
		return
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	b.WriteString("| Flag | Default | What it does |\n| --- | --- | --- |\n")
	for _, f := range flags {
		name := "`--" + f.Name + "`"
		if f.Shorthand != "" {
			name += ", `-" + f.Shorthand + "`"
		}
		def := ""
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			def = "`" + f.DefValue + "`"
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", name, def, mdx(f.Usage))
	}
	b.WriteString("\n")
}

// prose is the command's Long description, or its Short when it has
// none — the same fallback `--help` makes.
func prose(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return mdx(strings.TrimSpace(cmd.Long))
	}
	return mdx(cmd.Short)
}

// mdx escapes what MDX would otherwise read as JSX or an expression:
// help text says `<name>` and means the angle brackets.
func mdx(s string) string {
	r := strings.NewReplacer("<", "\\<", ">", "\\>", "{", "\\{", "}", "\\}", "|", "\\|")
	return r.Replace(s)
}

// writePages writes every page into dir, or with check compares and
// reports each one that differs.
func writePages(dir string, pages map[string]string, check bool) error {
	if !check {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	var stale []string
	for name, want := range pages {
		path := filepath.Join(dir, name)
		if check {
			have, err := os.ReadFile(path)
			if err != nil || string(have) != want {
				stale = append(stale, path)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			return err
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		return errors.New("stale CLI reference, run `make reference`:\n  " + strings.Join(stale, "\n  "))
	}
	return nil
}
