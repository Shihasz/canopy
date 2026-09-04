package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Shihasz/canopy/internal/config"
)

func newDeployCmd() *cobra.Command {
	var newVersion, priorVersion string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Start a progressive rollout of a new version to the canary host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if newVersion == "" {
				return fmt.Errorf("--version is required")
			}
			if priorVersion == "" {
				return fmt.Errorf("--prior-version is required")
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			return runDeploy(cmd.Context(), cfg, newVersion, priorVersion)
		},
	}

	cmd.Flags().StringVar(&newVersion, "version", "", "version to deploy (required)")
	cmd.Flags().StringVar(&priorVersion, "prior-version", "", "last known-good version, used for rollback (required)")

	return cmd
}
