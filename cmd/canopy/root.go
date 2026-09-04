package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// version is overridden at build time via -ldflags.
	version = "dev"

	// cfgPath is shared by all subcommands via a persistent flag.
	cfgPath string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "canopy",
		Short:   "Progressive delivery controller for VM-based deployments",
		Version: version,
	}
	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "canopy.yaml", "path to canopy config file")

	root.AddCommand(newDeployCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newRollbackCmd())
	root.AddCommand(newPromoteCmd())

	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
