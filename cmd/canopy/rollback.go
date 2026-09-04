package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Manually roll back a rollout to its prior version",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement once state persistence exists;
			// currently rollback happens automatically inside `deploy`
			// when canary analysis fails.
			return fmt.Errorf("rollback: not yet implemented as a standalone command; automatic rollback happens during deploy")
		},
	}
}
