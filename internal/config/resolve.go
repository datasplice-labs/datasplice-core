package config

import (
	"fmt"
	"regexp"
)

// interpRe matches ${NAME}. No nesting, no defaults, no expressions
var interpRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Resolved bundles a parsed, secret-resolved config: everything validate,
// plan, and run need before a single package is touched.
type Resolved struct {
	Main         *Main
	Warnings     []string
	SecretValues map[string]string // name -> value, for every ${NAME} referenced anywhere in Main
}

// LoadAndResolve parses main.yaml/secrets.yaml, picks the secrets block,
// and resolves every ${NAME} referenced in any step's `with:` block
// against it — all before anything spawns.
func LoadAndResolve(mainPath, secretsPath string) (*Resolved, error) {
	m, sf, err := Load(mainPath, secretsPath)
	if err != nil {
		return nil, err
	}

	block, err := ResolveSecrets(m, sf)
	if err != nil {
		return nil, err
	}

	var warnings []string
	if block.Type == EnvVarsTypePlain {
		warnings = append(warnings, `secrets type "plain" stores values inline. Remember to gitignore this file`)
	}

	names := referencedNames(m)
	values, err := block.Resolve(names)
	if err != nil {
		return nil, err
	}

	return &Resolved{Main: m, Warnings: warnings, SecretValues: values}, nil
}

func referencedNames(m *Main) []string {
	seen := map[string]bool{}
	var names []string

	for _, s := range m.Steps {
		for _, n := range append(scanRefs(s.With), s.Secrets...) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}

	return names
}

// StepSecrets returns the secret values a step may see: the names it
// lists under `secrets:` plus any it references as ${NAME} in `with:`.
func StepSecrets(s Step, values map[string]string) map[string]string {
	out := ReferencedValues(s.With, values)
	for _, n := range s.Secrets {
		if v, ok := values[n]; ok {
			out[n] = v
		}
	}

	return out
}

// Given a variable (any) return the list of referenced secret names.
func scanRefs(v any) []string {
	var names []string
	switch t := v.(type) {
	case string:
		for _, m := range interpRe.FindAllStringSubmatch(t, -1) {
			names = append(names, m[1])
		}
	case map[string]any:
		for _, vv := range t {
			names = append(names, scanRefs(vv)...)
		}
	case []any:
		for _, vv := range t {
			names = append(names, scanRefs(vv)...)
		}
	}

	return names
}

// ReferencedValues returns the subset of values referenced by with.
func ReferencedValues(with map[string]any, values map[string]string) map[string]string {
	out := map[string]string{}
	for _, n := range scanRefs(with) {
		if v, ok := values[n]; ok {
			out[n] = v
		}
	}
	return out
}

// Interpolate resolves every ${NAME} in with (recursively through nested
// maps/lists) against values. Interpolation happens only in `with:` — not
// in `uses:`, which must stay reproducible across environments.
func Interpolate(with map[string]any, values map[string]string) (map[string]any, error) {
	out, err := interpolateAny(with, values)
	if err != nil {
		return nil, err
	}

	if out == nil {
		return map[string]any{}, nil
	}

	return out.(map[string]any), nil
}

// Given a variable (any) and a map of values, interpolateAny returns the
// variable with every ${NAME} replaced by its value, or an error if any
// ${NAME} is missing.
func interpolateAny(v any, values map[string]string) (any, error) {
	switch t := v.(type) {
	case string:
		var missing error
		result := interpRe.ReplaceAllStringFunc(t, func(match string) string {
			name := interpRe.FindStringSubmatch(match)[1]
			val, ok := values[name]
			if !ok {
				missing = fmt.Errorf("unresolved variable ${%s}", name)
				return match
			}
			return val
		})

		if missing != nil {
			return nil, missing
		}

		return result, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			r, err := interpolateAny(vv, values)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}

		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			r, err := interpolateAny(vv, values)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}

		return out, nil
	default:
		return v, nil
	}
}
