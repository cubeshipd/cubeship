package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is stamped at link time by the release workflow; a build
// without that says so rather than claiming a number nobody released.
//
// It is a var and not a const because `-X main.version=` can only write
// a variable: as a const the linker flag was accepted and silently did
// nothing, so every CLI ever released reported the placeholder.
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "cubeship",
		Short:   "CLI for the Cubeship self-hosted deploy engine",
		Version: version,
	}

	root.AddCommand(newLoginCmd())
	root.AddCommand(newRegistryCmd())
	root.AddCommand(newAppCmd())
	root.AddCommand(newProjectCmd())
	root.AddCommand(newEnvironmentCmd())
	root.AddCommand(newDatastoreCmd())
	root.AddCommand(newServerCmd())
	root.AddCommand(newUserCmd())
	root.AddCommand(newVersionCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newVersionCmd is what somebody types. Cobra's Version field gives
// `--version` and no subcommand, so without this `cubeship version`
// answers "unknown command" — which is the wrong answer to the question
// every bug report starts with.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("cubeship %s\n", version)
		},
	}
}
