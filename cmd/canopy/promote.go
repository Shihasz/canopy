package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote",
		Short: "Manually promote a rollout to 100% traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement once state persistence exists;
			// currently promotion happens automatically inside `deploy`
			// after canary analysis passes at 100%.
			return fmt.Errorf("promote: not yet implemented as a standalone command; automatic promotion happens during deploy")
		},
	}
}
