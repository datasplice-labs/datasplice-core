package cmd

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/pipeline"
	"github.com/datasplice-labs/datasplice-core/internal/redact"
	"github.com/spf13/cobra"
)

var dryRun bool

// ponytail: no separate `init` command — package resolution/installation
// (docs/roadmap: `datasplice get`, M3) belongs right here, as one pass
// over every step before any row processing starts, so a missing package
// fails clean before anything's been written. Not implemented yet: M0/M1
// only know first-party builtins.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: fmt.Sprintf("Run the flow described by %s", MainFile),
	RunE: func(cmd *cobra.Command, args []string) error {
		resolved, err := config.LoadAndResolve(MainFile, VariablesFile)
		if err != nil {
			return err
		}
		steps, err := pipeline.Build(resolved.Main, resolved.SecretValues)
		if err != nil {
			return err
		}
		if err := pipeline.Configure(steps); err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		rw := redact.New(cmd.OutOrStdout(), resolved.SecretValues)

		if dryRun {
			count, sample, err := pipeline.RunDryRun(ctx, steps)
			if err != nil {
				return err
			}
			fmt.Fprintf(rw, "dry run: %d records would reach the sink\n\nsample:\n", count)
			for _, r := range sample {
				fmt.Fprintf(rw, "  %v\n", r)
			}
			return nil
		}
		return pipeline.Run(ctx, steps)
	},
}

func init() {
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "fetch and transform but don't write to the sink")
	rootCmd.AddCommand(runCmd)
}
