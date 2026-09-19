// Package manifest parses and validates datasplice.yaml — third-party
// package manifests that describe an HTTP API declaratively. No network
// calls here: fetching a manifest from GitHub is a resolver's job;
// this package only knows the file format.
package manifest

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SupportedManifestVersion is the only manifest_version this core
// understands right now.
const SupportedManifestVersion = 1

// Manifest is datasplice.yaml.
type Manifest struct {
	Name            string                 `yaml:"name"`
	Version         string                 `yaml:"version"`
	Description     string                 `yaml:"description,omitempty"`
	ManifestVersion int                    `yaml:"manifest_version"`
	Settings        map[string]SettingSpec `yaml:"settings,omitempty"`
	Secrets         map[string]SecretSpec  `yaml:"secrets,omitempty"`
	BaseURL         string                 `yaml:"base_url"`
	Auth            *Auth                  `yaml:"auth,omitempty"`
	RateLimit       *RateLimit             `yaml:"rate_limit,omitempty"`
	Retry           *Retry                 `yaml:"retry,omitempty"`
	Actions         map[string]Action      `yaml:"actions"`

	// Reserved for future use. Declared here (rather than left unknown)
	// so Load can reject them with a clear message instead of a generic
	// "unknown field" error.
	//
	// Those are custom fields that may be implemented by a package to
	// inform the core that there is a requirement to run a custom binary via
	// a hook, or to maintain state between steps.
	Hook  *any `yaml:"hook,omitempty"`
	State *any `yaml:"state,omitempty"`
}

// SettingSpec is one entry under `settings:`
// what a step may pass in `with:` for this manifest's actions.
type SettingSpec struct {
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required,omitempty"`
	OneOf       []string `yaml:"one_of,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// SecretSpec is one entry under `secrets:`
// a secret name this manifest needs, and why.
type SecretSpec struct {
	Description string `yaml:"description,omitempty"`
}

// Auth covers every non-signed auth type (basic/bearer/header/query/none).
// Which fields apply depends on Type; see validate.go.
type Auth struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Token    string `yaml:"token,omitempty"`
	Name     string `yaml:"name,omitempty"`  // header/query: the header or param name
	Value    string `yaml:"value,omitempty"` // header/query: its value
}

// RateLimit is the manifest-level budget, optionally overridden per action.
type RateLimit struct {
	Requests  int                  `yaml:"requests"`
	Per       string               `yaml:"per"`
	Overrides map[string]RateLimit `yaml:"overrides,omitempty"`
}

// Retry configures what the core does on a failed request.
type Retry struct {
	On                []int  `yaml:"on,omitempty"`
	RespectRetryAfter bool   `yaml:"respect_retry_after,omitempty"`
	MaxAttempts       int    `yaml:"max_attempts,omitempty"`
	Backoff           string `yaml:"backoff,omitempty"`
	InitialDelay      string `yaml:"initial_delay,omitempty"`
}

// Paginate describes how to fetch the next page. Which fields apply
// depends on Style (cursor/page/offset/link_header).
type Paginate struct {
	Style     string `yaml:"style"`
	Next      string `yaml:"next,omitempty"`       // cursor
	Until     string `yaml:"until,omitempty"`      // cursor/page/offset
	Param     string `yaml:"param,omitempty"`      // page/offset
	Start     int    `yaml:"start,omitempty"`      // page
	SizeParam string `yaml:"size_param,omitempty"` // offset
	Size      int    `yaml:"size,omitempty"`       // offset
}

// Batch declares how many records an action wants per call.
type Batch struct {
	Default int `yaml:"default,omitempty"`
	Max     int `yaml:"max,omitempty"`
}

// Action is one entry under `actions:` — one HTTP operation this package
// exposes, e.g. "show_many". Role decides how the core wires it into a
// flow (source/transform/sink), same meaning as contract.Role.
type Action struct {
	Role     string            `yaml:"role"`
	Method   string            `yaml:"method"`
	Path     string            `yaml:"path"`
	Query    map[string]any    `yaml:"query,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	Body     map[string]any    `yaml:"body,omitempty"`
	Records  string            `yaml:"records,omitempty"` // JSONPath to the array of records (source)
	Fields   map[string]string `yaml:"fields,omitempty"`  // derived field name -> JSONPath
	Output   map[string]string `yaml:"output,omitempty"`  // transform: response field -> JSONPath
	Paginate *Paginate         `yaml:"paginate,omitempty"`
	Batch    *Batch            `yaml:"batch,omitempty"`
}

// decodeStrict rejects unknown fields, same as internal/config's.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(out)
}

// Load reads, strictly parses, and validates a manifest file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// Decode the manifest inside Manifest, so we can validate it before returning.
	var m Manifest
	if err := decodeStrict(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Validate the manifest's fields, so we can return a clear error message
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &m, nil
}
