package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const version = "0.1.0-dev"

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
	root.AddCommand(newUserCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
