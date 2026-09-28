package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"

	"github.com/spf13/cobra"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/registry"
)

// getCmd is the only command that touches the network for package
// resolution — validate/plan/run stay offline and only ever read what
// get already cached and locked (see resolveThirdParty in
// internal/pipeline/manifestpkg.go).
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Fetch every third-party package the flow uses and write datasplice.lock",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		return runGet(ctx, cmd, &registry.Fetcher{})
	},
}

func init() {
	rootCmd.AddCommand(getCmd)
}

// runGet is getCmd's RunE, with the network client factored out so tests
// can point it at a fake GitHub instead of the real one.
func runGet(ctx context.Context, cmd *cobra.Command, fetcher *registry.Fetcher) error {
	m, err := config.LoadMain(MainFile)
	if err != nil {
		return err
	}

	refs, err := thirdPartyRefs(m)
	if err != nil {
		return err
	}

	if len(refs) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no third-party packages used")
		return err
	}

	home, err := registry.Home()
	if err != nil {
		return err
	}

	old, err := registry.LoadLockfile(registry.LockFile)
	if err != nil {
		return err
	}

	newLock := &registry.Lockfile{Entries: map[registry.Ref]string{}}
	for _, ref := range refs {
		// Get's own errors (via Fetcher) already name the ref — no need
		// to wrap it on again here.
		hash, err := registry.Get(ctx, fetcher, home, ref)
		if err != nil {
			return err
		}

		newLock.Entries[ref] = hash
	}

	if err := newLock.Save(registry.LockFile); err != nil {
		return err
	}

	for _, line := range diffLock(old, newLock) {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
			return err
		}
	}

	return nil
}

// thirdPartyRefs collects every distinct third-party ref across steps,
// in first-use order, so `get`'s output and any errors are predictable.
func thirdPartyRefs(m *config.Main) ([]registry.Ref, error) {
	seen := map[registry.Ref]bool{}

	var refs []registry.Ref
	for i, s := range m.Steps {
		if !registry.IsThirdParty(s.Uses) {
			continue
		}

		ref, err := registry.ParseRef(s.Uses)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}

		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	return refs, nil
}

// diffLock reports what changed at package (host/path) granularity, not
// per exact ref — bumping a version reads as "updated", not "removed
// old, added new".
func diffLock(old, newLock *registry.Lockfile) []string {
	oldPkg := versionsByPackage(old)
	newPkg := versionsByPackage(newLock)

	var lines []string
	for _, pkg := range sortedKeys(newPkg) {
		version := newPkg[pkg]
		switch prev, ok := oldPkg[pkg]; {
		case !ok:
			lines = append(lines, fmt.Sprintf("  + %s@%s", pkg, version))
		case prev != version:
			lines = append(lines, fmt.Sprintf("  ~ %s: %s -> %s", pkg, prev, version))
		}
	}

	for _, pkg := range sortedKeys(oldPkg) {
		if _, ok := newPkg[pkg]; !ok {
			lines = append(lines, fmt.Sprintf("  - %s@%s", pkg, oldPkg[pkg]))
		}
	}

	return lines
}

func versionsByPackage(l *registry.Lockfile) map[string]string {
	m := make(map[string]string, len(l.Entries))
	for ref := range l.Entries {
		m[ref.Host+"/"+ref.Path] = ref.Version
	}

	return m
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
