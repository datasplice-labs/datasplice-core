package cmd

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/datasplice-labs/datasplice-core/internal/redact"
)

const (
	// Name of the files required
	// for a datasplice run.
	MainFile    = "main.yaml"
	SecretsFile = "secrets.yaml"
)

var rootCmd = &cobra.Command{
	Use:   "datasplice",
	Short: "Ingest, transform and export data via a YAML-defined flow",

	// Usage text after a runtime failure (a 404, a bad file) is noise.
	SilenceUsage: true,

	// Version is the version of the datasplice core, printed by
	// `datasplice version` or `datasplice --version`.
	Version: "",
}

// redactErr masks secret values in err's message. cobra prints returned
// errors straight to stderr, and requests made with real credentials can
// produce errors that mention them.
func redactErr(err error, secrets map[string]string) error {
	if err == nil {
		return nil
	}

	return errors.New(redact.New(nil, secrets).String(err.Error()))
}

// Execute runs the root command; main just calls this.
func Execute(v string) {
	rootCmd.Version = v

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
