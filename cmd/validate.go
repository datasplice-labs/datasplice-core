package cmd

import (
	"fmt"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/manifest"
	"github.com/datasplice-labs/datasplice-core/internal/redact"
	"github.com/spf13/cobra"
)

var (
	// Path to the manifest file to validate, if any.
	manifestPath string
)

// validate never resolves packages or touches the network — it only
// proves the YAML parses, is free of unknown fields, and every secret
// reference resolves (datasplice-core-prd.md §3).
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Parse and schema-check the flow, without spawning any package",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --manifest checks a package manifest standalone, for package authors.
		if manifestPath != "" {
			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: valid (%d actions, manifest_version %d)\n", manifestPath, len(m.Actions), m.ManifestVersion)
			return err
		}

		resolved, err := config.LoadAndResolve(MainFile, SecretsFile)
		if err != nil {
			return err
		}
		rw := redact.New(cmd.OutOrStdout(), resolved.SecretValues)
		for _, w := range resolved.Warnings {
			if _, err := fmt.Fprintln(rw, "warning:", w); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(rw, "%s: valid (%d steps, secrets resolve)\n", MainFile, len(resolved.Main.Steps))
		return err
	},
}

func init() {
	validateCmd.Flags().StringVar(&manifestPath, "manifest", "", "validate a package manifest (datasplice.yaml) standalone, without main.yaml")
	rootCmd.AddCommand(validateCmd)
}
