package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current state of a rollout",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: query live rollout state once the
			// orchestrator's state is persisted/queryable against a
			// running environment. Deploy currently runs synchronously
			// and reports its own final state on completion.
			return fmt.Errorf("status: not yet implemented; deploy reports final rollout state directly")
		},
	}
}
