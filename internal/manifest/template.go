package manifest

import (
	"fmt"
	"regexp"
)

// tmplRe matches {{ scope.name }}. Only settings and secrets are handled
// here — `in` (the incoming record, transform actions only) is resolved
// later, during actual execution, once a real record exists.
var tmplRe = regexp.MustCompile(`\{\{\s*(settings|secrets)\.([A-Za-z0-9_]+)\s*\}\}`)

// Resolve substitutes every {{ settings.x }} / {{ secrets.X }} in s.
// allowSecrets should only be true for auth fields and headers.
// Load already enforces that restriction structurally (checkNoSecretsUsed),
// so this is the second half of the same rule, applied once real values are
// available. An unresolvable reference errors rather than being left as
// a literal placeholder or silently dropped.
func Resolve(s string, settings map[string]any, secrets map[string]string, allowSecrets bool) (string, error) {
	var err error
	result := tmplRe.ReplaceAllStringFunc(s, func(match string) string {
		if err != nil {
			return match
		}

		groups := tmplRe.FindStringSubmatch(match)
		scope, name := groups[1], groups[2]
		switch scope {
		case "settings":
			v, ok := settings[name]
			if !ok {
				err = fmt.Errorf("{{ settings.%s }}: not set", name)
				return match
			}
			return fmt.Sprint(v)
		case "secrets":
			if !allowSecrets {
				err = fmt.Errorf("{{ secrets.%s }}: secrets can only be referenced in auth and headers", name)
				return match
			}
			v, ok := secrets[name]
			if !ok {
				err = fmt.Errorf("{{ secrets.%s }}: not provided", name)
				return match
			}
			return v
		}
		return match
	})

	if err != nil {
		return "", err
	}

	return result, nil
}
