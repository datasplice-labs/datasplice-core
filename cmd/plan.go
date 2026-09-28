package cmd

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/pipeline"
	"github.com/datasplice-labs/datasplice-core/internal/redact"
	"github.com/datasplice-labs/datasplice-core/internal/registry"
)

// check is one line of plan's checklist.
type check struct {
	name    string
	detail  string // shown after a ✓
	err     error  // non-nil is a ✗; its message is shown indented below
	skipped bool   // couldn't run because an earlier check failed
}

// plan is a static check: does the pipeline compose at all, offline
// (datasplice-core-prd.md §3 "What plan means here"). It resolves every
// step's package and calls Describe, but never Configure or Process.
// Every check runs independently, so one pass shows every problem, and
// any failure makes the command exit non-zero (usable as a CI gate).
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Check the pipeline can run, offline",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		// Nothing can be checked if the config itself doesn't parse.
		m, sf, err := config.Load(MainFile, SecretsFile)
		if err != nil {
			return err
		}

		var secretValues map[string]string
		resolved, secretsErr := config.Resolve(m, sf)
		if resolved != nil {
			secretValues = resolved.SecretValues
		}

		defer func() {
			err = redactErr(err, secretValues)
		}()

		checks := []check{{name: "secrets resolve", err: secretsErr}}
		if secretsErr == nil {
			checks[0].detail = fmt.Sprintf("%d referenced", len(secretValues))
		}

		lockedCount, lockErr := checkLockfile(m)
		lock := check{name: "lockfile complete", err: lockErr, detail: "no third-party packages"}
		if lockedCount > 0 {
			lock.detail = fmt.Sprintf("%d third-party packages", lockedCount)
		}
		checks = append(checks, lock)

		// Build needs the secret values and resolves third-party packages
		// through the lockfile, so it only runs once both are fine.
		compose := check{name: "steps compose"}

		var steps []pipeline.Step
		if secretsErr != nil || lockErr != nil {
			compose.skipped = true
		} else {
			steps, compose.err = pipeline.Build(m, secretValues)
			compose.detail = fmt.Sprintf("%d steps", len(steps))
		}
		checks = append(checks, compose)

		rw := redact.New(cmd.OutOrStdout(), secretValues)
		if err := printPlan(rw, m.Name, steps, checks); err != nil {
			return err
		}

		failed := 0
		for _, check := range checks {
			if check.err != nil {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("plan failed: %d check(s) did not pass", failed)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
}

// checkLockfile verifies every third-party package the flow uses is in
// datasplice.lock, cached, and unedited. It returns how many packages
// it looked at, and every problem found, not just the first.
func checkLockfile(m *config.Main) (int, error) {
	refs, err := thirdPartyRefs(m)
	if err != nil || len(refs) == 0 {
		return 0, err
	}

	lock, err := registry.LoadLockfile(registry.LockFile)
	if err != nil {
		return len(refs), err
	}

	home, err := registry.Home()
	if err != nil {
		return len(refs), err
	}

	var errs []error
	for _, ref := range refs {
		if _, err := registry.Load(home, lock, ref); err != nil {
			errs = append(errs, err)
		}
	}

	return len(refs), errors.Join(errs...)
}

// printPlan writes the resolved steps (when they built) and the checklist.
// w must already be the redacting writer: a destination can carry an
// interpolated secret.
func printPlan(w io.Writer, name string, steps []pipeline.Step, checks []check) error {
	var stringBuilder strings.Builder

	fmt.Fprintf(&stringBuilder, "Pipeline: %s\n\n", name)

	for i, s := range steps {
		fmt.Fprintf(&stringBuilder, "  %d  %-10s %-10s %s\n", i+1, s.Describe.Name, s.Role, s.Uses)

		if s.Action != "" {
			fmt.Fprintf(&stringBuilder, "       action: %s\n", s.Action)
		}

		if s.Destination != "" {
			fmt.Fprintf(&stringBuilder, "       destination: %s\n", s.Destination)
		}

		// Names only, never values.
		if len(s.Secrets) > 0 {
			names := make([]string, 0, len(s.Secrets))
			for n := range s.Secrets {
				names = append(names, n)
			}

			sort.Strings(names)
			fmt.Fprintf(&stringBuilder, "       secrets: %s\n", strings.Join(names, ", "))
		}
	}

	if len(steps) > 0 {
		stringBuilder.WriteString("\n")
	}

	for _, check := range checks {
		switch {
		case check.skipped:
			fmt.Fprintf(&stringBuilder, "- %s (skipped: fix the failures above first)\n", check.name)
		case check.err != nil:
			fmt.Fprintf(&stringBuilder, "✗ %s\n", check.name)

			for line := range strings.SplitSeq(check.err.Error(), "\n") {
				fmt.Fprintf(&stringBuilder, "    %s\n", line)
			}
		default:
			fmt.Fprintf(&stringBuilder, "✓ %s (%s)\n", check.name, check.detail)
		}
	}

	_, err := io.WriteString(w, stringBuilder.String())

	return err
}
