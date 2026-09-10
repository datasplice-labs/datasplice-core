package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	// EnvVarsTypeNoEnv resolves to nothing — a missing secrets block is
	// always an error, so "no secrets" must be said explicitly.
	EnvVarsTypeNoEnv = "noenv"
	// EnvVarsTypePlain resolves from the `values` map inline in the file.
	// Local convenience only; Resolve's caller (LoadAndResolve) warns
	// every time one is loaded.
	EnvVarsTypePlain = "plain"
	// EnvVarsTypeFile resolves from a dotenv file at Filepath.
	EnvVarsTypeFile = "file"
	// EnvVarsTypeInjected resolves from the process environment. The
	// cloud path.
	EnvVarsTypeInjected = "injected"
)

// Resolve looks up exactly the secret names referenced anywhere in
// main.yaml's `with:` blocks (see referencedNames) — never more, so
// nothing outside those ${NAME} refs ever enters the process.
func (e *Secrets) Resolve(names []string) (map[string]string, error) {
	switch e.Type {
	case EnvVarsTypeNoEnv:
		if len(names) > 0 {
			return nil, fmt.Errorf("secrets: type is %q but %v are referenced", EnvVarsTypeNoEnv, names)
		}

		return map[string]string{}, nil
	case EnvVarsTypePlain:
		return lookup(names, e.Values, "declared in `secrets.values`")
	case EnvVarsTypeFile:
		values, err := parseDotenv(e.Filepath)
		if err != nil {
			return nil, err
		}
		return lookup(names, values, fmt.Sprintf("found in %s", e.Filepath))
	case EnvVarsTypeInjected:
		values := map[string]string{}
		for _, n := range names {
			if v, ok := os.LookupEnv(n); ok {
				values[n] = v
			}
		}
		return lookup(names, values, "set in the environment")
	default:
		return nil, fmt.Errorf("secrets: unknown type %q (accepted: %s | %s | %s | %s)",
			e.Type, EnvVarsTypeNoEnv, EnvVarsTypePlain, EnvVarsTypeFile, EnvVarsTypeInjected)
	}
}

// Given a list of names and a map of values, lookup returns a map of the
// names to their values, or an error (provided) if any name is missing.
func lookup(names []string, values map[string]string, sourceDesc string) (map[string]string, error) {
	resolved := make(map[string]string, len(names))

	for _, n := range names {
		v, ok := values[n]
		if !ok {
			return nil, fmt.Errorf("secrets: %q is not %s", n, sourceDesc)
		}

		resolved[n] = v
	}

	return resolved, nil
}

// parseDotenv reads a minimal KEY=VALUE file; no need for a parsing
// dependency for something this small.
func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: error reading %s: %w", path, err)
	}

	values := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}

	return values, nil
}
