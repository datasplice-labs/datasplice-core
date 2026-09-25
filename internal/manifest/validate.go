package manifest

import (
	"fmt"
	"regexp"
	"slices"
)

var validSettingTypes = map[string]bool{"string": true, "number": true, "bool": true}
var validAuthTypes = map[string]bool{"": true, "none": true, "basic": true, "bearer": true, "header": true, "query": true}
var validRoles = map[string]bool{"source": true, "transform": true, "sink": true}

// Validate checks everything that doesn't depend on a step's `with:`
//
// manifest_version, reserved keys, settings/action shapes, and that
// secrets are only referenced where they're allowed to be. Load calls
// this on every manifest file; a caller building a *Manifest in memory
// (e.g. the datasplice/http builtin, from its own `with:` block) can
// call it directly to get the same checks.
func (m *Manifest) Validate() error {
	if m.ManifestVersion != SupportedManifestVersion {
		return fmt.Errorf("manifest_version: %d is not supported (supported: %d)", m.ManifestVersion, SupportedManifestVersion)
	}

	// For both State and Hook, we have simply prepared the initial field logic
	// for future use. We will reject them if they are present, since the mechanism
	// is missing, and we don't want anything that isn't fully implemented to be included.

	if m.Hook != nil {
		return fmt.Errorf("hook: reserved for a future version")
	}
	if m.State != nil {
		return fmt.Errorf("state: reserved for a future version")
	}

	// A manifest with no action is useless, so we require at least one.
	// The core doesn't care about the action's name, so we don't either.
	if len(m.Actions) == 0 {
		return fmt.Errorf("at least one action is required")
	}

	for name, s := range m.Settings {
		// If a setting is declared, it must be a valid type (string, number, or bool).
		if !validSettingTypes[s.Type] {
			return fmt.Errorf("settings.%s: type %q is invalid (want string, number, or bool)", name, s.Type)
		}

		// If a setting has a one_of list, it must be a string.
		// one_of represents a list of elements that a setting may be, and is only valid for string settings.
		if len(s.OneOf) > 0 && s.Type != "string" {
			return fmt.Errorf("settings.%s: one_of is only valid on type string", name)
		}
	}

	if m.Auth != nil && !validAuthTypes[m.Auth.Type] {
		return fmt.Errorf("auth.type %q is not allowed (want none, basic, bearer, header, or query)", m.Auth.Type)
	}

	// We cannot have a secret reference in the base_url, because it would end up in a URL, a log line, or a request body.
	if err := checkNoSecretsUsed("base_url", m.BaseURL); err != nil {
		return err
	}

	// Now, validate all actions from the manifest.
	// Each action must have a valid role, and secrets may only be referenced in auth and headers.
	for name, a := range m.Actions {
		if !validRoles[a.Role] {
			return fmt.Errorf("actions.%s: role %q is invalid (want source, transform, or sink)", name, a.Role)
		}

		if err := a.checkSecretScope(name); err != nil {
			return err
		}
	}

	return m.validateHTTP()
}

// secretRe matches {{ secrets.NAME }} — used to keep secrets out of
// fields that aren't auth or headers, since they'd otherwise end up in a
// URL, a log line, or a request body.
var secretRe = regexp.MustCompile(`\{\{\s*secrets\.`)

func checkNoSecretsUsed(field, s string) error {
	if secretRe.MatchString(s) {
		return fmt.Errorf("%s: secrets can only be referenced in auth and headers", field)
	}

	return nil
}

// checkSecretScope rejects {{ secrets.X }} in every field of an action
// except auth and headers (auth itself is checked once, manifest-wide,
// not per action).
func (a Action) checkSecretScope(name string) error {
	if err := checkNoSecretsUsed(fmt.Sprintf("actions.%s.path", name), a.Path); err != nil {
		return err
	}

	if err := checkNoSecretsUsed(fmt.Sprintf("actions.%s.records", name), a.Records); err != nil {
		return err
	}

	if err := walkStrings(a.Query, func(s string) error {
		return checkNoSecretsUsed(fmt.Sprintf("actions.%s.query", name), s)
	}); err != nil {
		return err
	}

	if err := walkStrings(a.Body, func(s string) error {
		return checkNoSecretsUsed(fmt.Sprintf("actions.%s.body", name), s)
	}); err != nil {
		return err
	}

	for field, path := range a.Fields {
		if err := checkNoSecretsUsed(fmt.Sprintf("actions.%s.fields.%s", name, field), path); err != nil {
			return err
		}
	}

	for field, path := range a.Output {
		if err := checkNoSecretsUsed(fmt.Sprintf("actions.%s.output.%s", name, field), path); err != nil {
			return err
		}
	}

	return nil
}

// walkStrings calls fn on every string found in v, recursing through
// maps and slices — used to scan an action's arbitrarily-nested body.
func walkStrings(v any, fn func(string) error) error {
	switch t := v.(type) {
	case string:
		return fn(t)
	case map[string]any:
		for _, vv := range t {
			if err := walkStrings(vv, fn); err != nil {
				return err
			}
		}
	case []any:
		for _, vv := range t {
			if err := walkStrings(vv, fn); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateWith checks with (a step's interpolated `with:` block) against
// this manifest's settings: every required setting present, every given
// setting's type correct, one_of respected.
func (m *Manifest) ValidateWith(with map[string]any) error {
	for name, spec := range m.Settings {
		v, ok := with[name]
		if !ok {
			if spec.Required {
				return fmt.Errorf("with.%s is required", name)
			}
			continue
		}

		if err := checkSettingType(name, v, spec.Type); err != nil {
			return err
		}

		if len(spec.OneOf) > 0 {
			s, _ := v.(string)
			if !slices.Contains(spec.OneOf, s) {
				return fmt.Errorf("with.%s: %q is not one of %v", name, s, spec.OneOf)
			}
		}
	}

	return nil
}

func checkSettingType(name string, v any, want string) error {
	ok := false
	switch want {
	case "string":
		_, ok = v.(string)
	case "number":
		// yaml.v3 decodes whole numbers as int, JSON as float64.
		switch v.(type) {
		case int, int64, float64:
			ok = true
		}
	case "bool":
		_, ok = v.(bool)
	}

	if !ok {
		return fmt.Errorf("with.%s: must be a %s", name, want)
	}

	return nil
}

// ValidateSecretsCovered checks that declared (a step's `secrets:` list)
// covers every secret this manifest needs.
func (m *Manifest) ValidateSecretsCovered(declared []string) error {
	have := make(map[string]bool, len(declared))
	for _, d := range declared {
		have[d] = true
	}

	for name := range m.Secrets {
		if !have[name] {
			return fmt.Errorf("secrets: %q is required by this package but not listed in the step's secrets", name)
		}
	}

	return nil
}
