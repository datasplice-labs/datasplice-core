package builtin

import (
	"context"
	"fmt"
	"maps"
	"net/url"

	"gopkg.in/yaml.v3"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/httpengine"
	"github.com/datasplice-labs/datasplice-core/internal/manifest"
)

// HTTP is a source builtin for a single endpoint that doesn't have a
// package yet: no datasplice.yaml on disk, just a `with:` block. It's
// not a second HTTP implementation — Configure builds a one-action
// in-memory *manifest.Manifest straight from `with:` and hands it to the
// same httpengine that runs published/local manifest packages, so
// auth/pagination/retry/rate-limit/JSONPath behave identically either
// way.
//
// `with:` uses the manifest vocabulary directly (records/fields are
// JSONPath, paginate/auth/rate_limit/retry match manifest.go's shapes)
// rather than reinventing one — see docs/getting-started/manifest-specs.md.
type HTTP struct {
	maxRecords int
	pkg        *httpengine.SourcePackage
}

func NewHTTP() *HTTP { return &HTTP{} }

// SetMaxRecords lets pipeline.Build wire a step's `max_records:` through
// — see maxRecordsSetter in internal/pipeline/manifestpkg.go. Must be
// called before Configure, same as `with:`/`secrets:` are only known by
// Configure time.
func (h *HTTP) SetMaxRecords(n int) { h.maxRecords = n }

func (h *HTTP) Describe() contract.Describe {
	return contract.Describe{
		Name: "http", Version: "0.2.0", Roles: []contract.Role{contract.RoleSource},
		Settings: []contract.SettingSpec{{Key: "url", Type: "string", Required: true}},
	}
}

func (h *HTTP) Configure(settings map[string]any, secrets map[string]string) error {
	m, err := manifestFromWith(settings)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}

	if err := m.Validate(); err != nil {
		return fmt.Errorf("http: %w", err)
	}

	pkg, err := httpengine.NewSourcePackage(m, "fetch", h.maxRecords)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}

	// Values from `with:` are already-resolved literals (interpolated via
	// ${NAME} before Configure ran, same as every builtin) — there's
	// nothing left for the engine to resolve against settings/secrets.
	if err := pkg.Configure(nil, nil); err != nil {
		return err
	}

	h.pkg = pkg

	return nil
}

func (h *HTTP) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	return h.pkg.Process(ctx, in, out)
}

// manifestFromWith builds the single-action manifest Configure needs.
// `with.url` is the whole endpoint (scheme, host, path, and any query
// string); everything else mirrors a real datasplice.yaml action.
func manifestFromWith(with map[string]any) (*manifest.Manifest, error) {
	raw, _ := with["url"].(string)
	if raw == "" {
		return nil, fmt.Errorf("`with.url` is required")
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("`with.url` must be an absolute http(s) URL")
	}

	action := manifest.Action{Role: "source", Method: "GET", Path: u.Path}
	if action.Path == "" {
		action.Path = "/" // a bare host (e.g. "https://api.example.com") means the root
	}
	if method, ok := with["method"].(string); ok && method != "" {
		action.Method = method
	}

	action.Query = map[string]any{}
	for k, v := range u.Query() {
		action.Query[k] = v[0]
	}

	if q, ok := with["query"].(map[string]any); ok {
		maps.Copy(action.Query, q)
	}

	if headers, ok := with["headers"].(map[string]any); ok {
		action.Headers = map[string]string{}
		for k, v := range headers {
			action.Headers[k] = fmt.Sprint(v)
		}
	}

	if recs, ok := with["records"].(string); ok {
		action.Records = recs
	}

	if fields, ok := with["fields"].(map[string]any); ok {
		action.Fields = map[string]string{}
		for k, v := range fields {
			if s, ok := v.(string); ok {
				action.Fields[k] = s
			}
		}
	}

	if p, ok := with["paginate"]; ok {
		if action.Paginate, err = decodeSubField[manifest.Paginate](p); err != nil {
			return nil, fmt.Errorf("paginate: %w", err)
		}
	}

	m := &manifest.Manifest{
		Name: "http", Version: "0.0.0", ManifestVersion: manifest.SupportedManifestVersion,
		BaseURL: u.Scheme + "://" + u.Host,
		Actions: map[string]manifest.Action{"fetch": action},
	}

	if a, ok := with["auth"]; ok {
		if m.Auth, err = decodeSubField[manifest.Auth](a); err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
	}

	if rl, ok := with["rate_limit"]; ok {
		if m.RateLimit, err = decodeSubField[manifest.RateLimit](rl); err != nil {
			return nil, fmt.Errorf("rate_limit: %w", err)
		}
	}

	if rt, ok := with["retry"]; ok {
		if m.Retry, err = decodeSubField[manifest.Retry](rt); err != nil {
			return nil, fmt.Errorf("retry: %w", err)
		}
	}

	return m, nil
}

// decodeSubField turns a `with:` fragment (whatever YAML/JSON produced —
// a map[string]any) into one of manifest's own typed structs, reusing
// its yaml tags instead of hand-writing a parallel set of type
// assertions for every field.
func decodeSubField[T any](v any) (*T, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}

	var out T
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
