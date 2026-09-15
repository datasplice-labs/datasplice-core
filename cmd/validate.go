package cmd

import (
	"fmt"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/spf13/cobra"
)

// validate never resolves packages or touches the network — it only
// proves the YAML parses, is free of unknown fields, and every secret
// reference resolves (datasplice-core-prd.md §3).
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Parse and schema-check the flow, without spawning any package",
	RunE: func(cmd *cobra.Command, args []string) error {
		resolved, err := config.LoadAndResolve(MainFile, SecretsFile)
		if err != nil {
			return err
		}
		for _, w := range resolved.Warnings {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "warning:", w); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: valid (%d steps, secrets resolve)\n", MainFile, len(resolved.Main.Steps))
		return err
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
