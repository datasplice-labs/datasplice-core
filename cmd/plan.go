package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/pipeline"
	"github.com/datasplice-labs/datasplice-core/internal/redact"
)

// plan is a static check: does the pipeline compose at all, offline
// (datasplice-core-prd.md §3 "What plan means here"). It resolves every
// step's package and calls Describe, but never Configure or Process.
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Check the pipeline can run, offline",
	RunE: func(cmd *cobra.Command, args []string) error {
		resolved, err := config.LoadAndResolve(MainFile, SecretsFile)
		if err != nil {
			return err
		}
		steps, err := pipeline.Build(resolved.Main, resolved.SecretValues)
		if err != nil {
			return err
		}

		rw := redact.New(cmd.OutOrStdout(), resolved.SecretValues)
		if _, err := fmt.Fprintf(rw, "Pipeline: %s\n\n", resolved.Main.Name); err != nil {
			return err
		}
		for i, s := range steps {
			if _, err := fmt.Fprintf(rw, "  %d  %-10s %-10s %s\n", i+1, s.Describe.Name, s.Role, s.Uses); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(rw, "\n✓ %d steps, roles compose\n✓ secrets resolve (%d referenced)\n", len(steps), len(resolved.SecretValues))
		return err
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
}
