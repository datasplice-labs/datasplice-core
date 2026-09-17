package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/pipeline"
	"github.com/datasplice-labs/datasplice-core/internal/redact"
	"github.com/spf13/cobra"
)

var dryRun bool

// package resolution/installation belongs right here, as one pass
// over every step before any row processing starts, so a missing package
// fails clean before anything's been written.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: fmt.Sprintf("Run the flow described by %s", MainFile),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load and resolve the main and secrets files, then build and configure the pipeline.
		resolved, err := config.LoadAndResolve(MainFile, SecretsFile)
		if err != nil {
			return err
		}

		// Build the pipeline steps from the resolved config
		// making sure that the secrets are correctly interpolated into the step configurations
		// and that the pipeline shape rules are followed (1 in, 1 out, X transforms).
		steps, err := pipeline.Build(resolved.Main, resolved.SecretValues)
		if err != nil {
			return err
		}
		if err := pipeline.Configure(steps); err != nil {
			return err
		}

		// Wraps ctx so Ctrl-C cancels it instead of killing the process outright
		// that's what every step's Process is already watching for via ctx.Done(),
		// so this is what makes an interrupt shut the pipeline down cleanly.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		rw := redact.New(cmd.OutOrStdout(), resolved.SecretValues)

		if dryRun {
			count, sample, err := pipeline.RunDryRun(ctx, steps)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(rw, "dry run: %d records would reach the sink\n\nsample:\n", count); err != nil {
				return err
			}
			for _, r := range sample {
				if _, err := fmt.Fprintf(rw, "  %v\n", r); err != nil {
					return err
				}
			}
			return nil
		}

		start := time.Now()
		count, err := pipeline.Run(ctx, steps)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(rw, "✓ %d records in %.1fs\n", count, time.Since(start).Seconds())
		return err
	},
}

func init() {
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "fetch and transform but don't write to the sink")
	rootCmd.AddCommand(runCmd)
}
