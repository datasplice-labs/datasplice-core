// Package config loads and resolves main.yaml/variables.yaml
// It only knows the shape of the YAML. It doesn't resolve packages or run a flow
// (see internal/pipeline for that).
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Step is the one shape every step has: `uses` plus `with`, and
// optionally `fn`/`on` for transforms. Role comes from the package's
// Describe response, not from which keys are set.
type Step struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with,omitempty"`
	Fn   string         `yaml:"fn,omitempty"`
	On   []string       `yaml:"on,omitempty"`
}

// Secrets mirrors the `secrets` block, whether it lives in main.yaml or
// variables.yaml — same shape either way.
type Secrets struct {
	Type     string            `yaml:"type"`
	Filepath string            `yaml:"filepath,omitempty"`
	Values   map[string]string `yaml:"values,omitempty"` // plain only
}

// Main is main.yaml. Four top-level keys and nothing else — unknown
// fields are rejected (see decodeStrict).
type Main struct {
	Name     string         `yaml:"name"`
	Steps    []Step         `yaml:"steps"`
	Secrets  *Secrets       `yaml:"secrets,omitempty"`
	Settings map[string]any `yaml:"settings,omitempty"` // reserved for state backend config; unused so far
}

type Variables struct {
	Secrets *Secrets `yaml:"secrets"`
}

// decodeStrict rejects unknown fields anywhere in the struct tree — a
// typo'd `slect:` must fail loudly rather than being silently ignored
// (datasplice-core-prd.md §2). map[string]any fields such as Step.With
// are exempt by construction: they accept any key, since `with:` is the
// package's business, not the core's.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	return dec.Decode(out)
}

func LoadMain(path string) (*Main, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var m Main
	if err := decodeStrict(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if len(m.Steps) == 0 {
		return nil, fmt.Errorf("%s: at least one step is required", path)
	}

	for i, s := range m.Steps {
		if s.Uses == "" {
			return nil, fmt.Errorf("%s: step %d: `uses` is required", path, i+1)
		}
	}

	return &m, nil
}

// LoadVariables returns (nil, nil) when the file doesn't exist — it's
// optional.
func LoadVariables(path string) (*Variables, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var v Variables
	if err := decodeStrict(data, &v); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &v, nil
}

// Load reads both files; variables.yaml is optional.
func Load(mainPath, varsPath string) (*Main, *Variables, error) {
	m, err := LoadMain(mainPath)
	if err != nil {
		return nil, nil, err
	}

	v, err := LoadVariables(varsPath)
	if err != nil {
		return nil, nil, err
	}

	return m, v, nil
}

// ResolveSecrets implements the precedence from datasplice-core-prd.md §2:
// main.yaml wins if present, then variables.yaml; it's an error to define
// it in both, or in neither.
func ResolveSecrets(m *Main, v *Variables) (*Secrets, error) {
	mainHas := m.Secrets != nil
	varsHas := v != nil && v.Secrets != nil

	switch {
	case mainHas && varsHas:
		return nil, fmt.Errorf("secrets cannot be defined in both main.yaml and variables.yaml")
	case mainHas:
		return m.Secrets, nil
	case varsHas:
		return v.Secrets, nil
	default:
		return nil, fmt.Errorf("secrets is missing: set type: noenv or define it in main.yaml or variables.yaml")
	}
}
