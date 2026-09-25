package httpengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/datasplice-labs/datasplice-core/internal/manifest"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

const (
	maxBodyBytes  = 64 << 20
	errBodyBytes  = 200
	clientTimeout = time.Minute
)

// Options tune an Engine; the zero value is fine.
type Options struct {
	// MaxRecords stops the run after this many records, even mid-page.
	MaxRecords int

	// Client overrides the default HTTP client (tests).
	Client *http.Client

	// Wait overrides how retry delays are slept (tests).
	Wait func(ctx context.Context, d time.Duration) error
}

// Engine runs one source action of a manifest. Everything templated is
// resolved once in New, so a bad setting or missing secret fails before
// any request is made.
type Engine struct {
	action     manifest.Action
	client     *http.Client
	wait       func(ctx context.Context, d time.Duration) error
	limiter    *rate.Limiter // nil: unlimited
	retry      retryPolicy
	maxRecords int

	base    *url.URL // base_url; every followed URL must stay on its host
	first   *url.URL // first request: base + path + query
	headers map[string]string
	auth    resolvedAuth

	records   *Path // nil: the response itself is the array
	fields    map[string]*Path
	until     manifest.Until
	untilPath *Path
	nextPath  *Path
	pageParam string
	page      int // first page number, for style page
}

// resolvedAuth is manifest.Auth with its templates filled in.
type resolvedAuth struct {
	kind               string
	username, password string
	token, name, value string
}

// New builds an Engine for one action, resolving every {{ }} template
// against the step's settings and granted secrets.
func New(manifest *manifest.Manifest, actionName string, settings map[string]any, secrets map[string]string, opts Options) (*Engine, error) {
	action, ok := manifest.Actions[actionName]
	if !ok {
		return nil, fmt.Errorf("package %s has no action %q", manifest.Name, actionName)
	}

	if action.Role != "source" {
		return nil, fmt.Errorf("action %q is a %s: only source actions are accepted", actionName, action.Role)
	}

	engine := &Engine{
		action:     action,
		maxRecords: opts.MaxRecords,
		retry:      newRetryPolicy(manifest.Retry),
	}

	engine.client = opts.Client
	if engine.client == nil {
		engine.client = &http.Client{Timeout: clientTimeout}
	}

	engine.wait = opts.Wait
	if engine.wait == nil {
		engine.wait = sleepCtx
	}

	if err := engine.buildRequest(manifest, settings, secrets); err != nil {
		return nil, err
	}

	if err := engine.buildExtraction(settings); err != nil {
		return nil, err
	}

	engine.limiter = newLimiter(manifest.RateLimit, actionName)

	// Refuse redirects off the base host: custom auth headers would
	// otherwise follow them.
	base := engine.client.CheckRedirect
	engine.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != engine.base.Host {
			return fmt.Errorf("redirect to a different host (%s) refused", req.URL.Host)
		}

		if base != nil {
			return base(req, via)
		}

		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}

		return nil
	}

	return engine, nil
}

func newLimiter(rl *manifest.RateLimit, action string) *rate.Limiter {
	if rl == nil {
		return nil
	}

	limit := rl
	if o, ok := rl.Overrides[action]; ok {
		limit = &o
	}

	per := map[string]time.Duration{"second": time.Second, "minute": time.Minute, "hour": time.Hour}[limit.Per]

	// Burst of 1 spaces requests evenly instead of allowing a burst.
	return rate.NewLimiter(rate.Every(per/time.Duration(limit.Requests)), 1)
}

func (engine *Engine) buildRequest(m *manifest.Manifest, settings map[string]any, secrets map[string]string) error {
	// Resolve every {{ }} template in the action.
	// Secrets are only allowed in auth and headers, which is enforced by the manifest loader.
	plain := func(s string) (string, error) {
		return manifest.Resolve(s, settings, secrets, false)
	}

	baseStr, err := plain(m.BaseURL)
	if err != nil {
		return fmt.Errorf("base_url: %w", err)
	}

	engine.base, err = url.Parse(baseStr)
	if err != nil || (engine.base.Scheme != "http" && engine.base.Scheme != "https") || engine.base.Host == "" {
		return fmt.Errorf("base_url %q must be an absolute http(s) URL", baseStr)
	}

	path, err := plain(engine.action.Path)
	if err != nil {
		return fmt.Errorf("path: %w", err)
	}

	first := *engine.base
	first.Path = strings.TrimRight(engine.base.Path, "/") + "/" + strings.TrimLeft(path, "/")

	q := url.Values{}
	for k, v := range engine.action.Query {
		s := fmt.Sprint(v)
		if str, ok := v.(string); ok {
			if s, err = plain(str); err != nil {
				return fmt.Errorf("query.%s: %w", k, err)
			}
		}

		q.Set(k, s)
	}

	if p := engine.action.Paginate; p != nil && p.Style == "page" {
		engine.pageParam = p.Param
		engine.page = p.Start
		if engine.page == 0 {
			engine.page = 1
		}

		q.Set(engine.pageParam, fmt.Sprint(engine.page))
	}

	first.RawQuery = q.Encode()
	engine.first = &first

	engine.headers = make(map[string]string, len(engine.action.Headers))
	for k, v := range engine.action.Headers {
		if engine.headers[k], err = manifest.Resolve(v, settings, secrets, true); err != nil {
			return fmt.Errorf("headers.%s: %w", k, err)
		}
	}

	return engine.buildAuth(m.Auth, settings, secrets)
}

func (engine *Engine) buildAuth(a *manifest.Auth, settings map[string]any, secrets map[string]string) error {
	if a == nil || a.Type == "" || a.Type == "none" {
		return nil
	}

	// Auth is one of the two places secrets may appear.
	resolve := func(field, s string) (string, error) {
		v, err := manifest.Resolve(s, settings, secrets, true)
		if err != nil {
			return "", fmt.Errorf("auth.%s: %w", field, err)
		}

		return v, nil
	}

	var err error
	engine.auth.kind = a.Type
	switch a.Type {
	case "basic":
		if engine.auth.username, err = resolve("username", a.Username); err != nil {
			return err
		}

		engine.auth.password, err = resolve("password", a.Password)
	case "bearer":
		engine.auth.token, err = resolve("token", a.Token)
	case "header", "query":
		if engine.auth.name, err = resolve("name", a.Name); err != nil {
			return err
		}

		engine.auth.value, err = resolve("value", a.Value)
	}

	return err
}

func (engine *Engine) buildExtraction(settings map[string]any) error {
	compile := func(field, expr string) (*Path, error) {
		s, err := manifest.Resolve(expr, settings, nil, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}

		p, err := CompilePath(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}

		return p, nil
	}

	var err error
	if engine.action.Records != "" {
		if engine.records, err = compile("records", engine.action.Records); err != nil {
			return err
		}
	}

	engine.fields = make(map[string]*Path, len(engine.action.Fields))
	for name, expr := range engine.action.Fields {
		if engine.fields[name], err = compile("fields."+name, expr); err != nil {
			return err
		}
	}

	p := engine.action.Paginate
	if p == nil {
		return nil
	}

	// Load already validated these parse.
	engine.until, _ = manifest.ParseUntil(p.Until)
	if engine.until.Kind == manifest.UntilCompare {
		if engine.untilPath, err = compile("paginate.until", engine.until.Path); err != nil {
			return err
		}
	}

	if p.Style == "cursor" {
		if engine.nextPath, err = compile("paginate.next", p.Next); err != nil {
			return err
		}
	}

	return nil
}

// Run fetches pages until pagination ends, calling emit with each page's
// records. It stops early if emit fails or MaxRecords is reached.
func (engine *Engine) Run(ctx context.Context, emit func([]record.Record) error) error {
	next, page, emitted := engine.first, engine.page, 0

	for {
		body, err := engine.get(ctx, next, emitted)
		if err != nil {
			return err
		}

		var doc any
		if err := json.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("GET %s: response is not JSON (%d records emitted): %w", next, emitted, err)
		}

		recs, err := engine.extract(doc)
		if err != nil {
			return fmt.Errorf("GET %s: %w", next, err)
		}

		seen := len(recs)
		capped := engine.maxRecords > 0 && emitted+seen >= engine.maxRecords
		if capped {
			recs = recs[:engine.maxRecords-emitted]
		}

		if len(recs) > 0 {
			if err := emit(recs); err != nil {
				return err
			}

			emitted += len(recs)
		}

		if capped {
			return nil
		}

		nu, np, more, err := engine.nextPage(doc, seen, next, page)
		if err != nil {
			return fmt.Errorf("GET %s: pagination: %w", next, err)
		}

		if !more {
			return nil
		}

		next, page = nu, np
	}
}

// get performs one request with rate limiting and retries. emitted is
// only used to make errors say how far the run got.
func (engine *Engine) get(ctx context.Context, u *url.URL, emitted int) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		if engine.limiter != nil {
			if err := engine.limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		fail := &RequestError{Method: engine.action.Method, URL: u.String(), Attempts: attempt, Emitted: emitted}

		resp, err := engine.do(ctx, u)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			fail.Err = err
			return nil, fail
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			fail.Err = err
			return nil, fail
		}

		if resp.StatusCode < 400 {
			return body, nil
		}

		fail.Status = resp.StatusCode
		fail.Body = snippet(body)
		if !engine.retry.shouldRetry(resp.StatusCode) || attempt >= engine.retry.maxAttempts {
			return nil, fail
		}

		if err := engine.wait(ctx, engine.retry.delay(attempt, resp.Header, time.Now())); err != nil {
			return nil, err
		}
	}
}

// do sends one request. Errors are stripped of the request URL, since
// query-string auth would otherwise end up in them.
func (engine *Engine) do(ctx context.Context, u *url.URL) (*http.Response, error) {
	reqURL := *u
	if engine.auth.kind == "query" {
		q := reqURL.Query()
		q.Set(engine.auth.name, engine.auth.value)
		reqURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, engine.action.Method, reqURL.String(), nil)
	if err != nil {
		return nil, errors.New("building request failed")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "datasplice-core")
	for k, v := range engine.headers {
		req.Header.Set(k, v)
	}

	switch engine.auth.kind {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+engine.auth.token)
	case "basic":
		req.SetBasicAuth(engine.auth.username, engine.auth.password)
	case "header":
		req.Header.Set(engine.auth.name, engine.auth.value)
	}

	resp, err := engine.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}

		return nil, err
	}

	return resp, nil
}

func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > errBodyBytes {
		s = s[:errBodyBytes] + "..."
	}

	return s
}

// extract pulls the records out of a response: exactly one match for the
// records path, which must be an array of objects, plus derived fields.
func (e *Engine) extract(doc any) ([]record.Record, error) {
	target, where := doc, "the response"
	if e.records != nil {
		matches := e.records.Select(doc)
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("records path %s matched nothing", e.records)
		case 1:
			target, where = matches[0], "records path "+e.records.String()
		default:
			return nil, fmt.Errorf("records path %s matched %d values, want one array", e.records, len(matches))
		}
	}

	items, ok := target.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is %s, want an array", where, typeName(target))
	}

	recs := make([]record.Record, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] is %s, want an object", where, i, typeName(item))
		}

		for name, p := range e.fields {
			v, _ := p.One(obj)
			obj[name] = v
		}

		recs[i] = record.Record(obj)
	}

	return recs, nil
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	}

	return fmt.Sprintf("%T", v)
}

// nextPage decides whether to keep going and where to. seen is how many
// records the page held before any MaxRecords trimming.
func (e *Engine) nextPage(doc any, seen int, cur *url.URL, page int) (*url.URL, int, bool, error) {
	p := e.action.Paginate
	if p == nil {
		return nil, page, false, nil
	}

	until := e.until
	if until.Kind == manifest.UntilNone && p.Style == "page" {
		until.Kind = manifest.UntilEmpty // page style has no other way to end
	}

	switch until.Kind {
	case manifest.UntilEmpty:
		if seen == 0 {
			return nil, page, false, nil
		}
	case manifest.UntilCompare:
		if v, ok := e.untilPath.One(doc); ok && reflect.DeepEqual(v, until.Value) == until.Equal {
			return nil, page, false, nil
		}
	}

	if p.Style == "page" {
		page++
		u := *e.first
		q := u.Query()
		q.Set(e.pageParam, fmt.Sprint(page))
		u.RawQuery = q.Encode()

		return &u, page, true, nil
	}

	return e.followCursor(doc, cur, page)
}

// followCursor reads the next URL out of the response. It must stay on the
// base host (the request carries credentials) and must actually move.
func (e *Engine) followCursor(doc any, cur *url.URL, page int) (*url.URL, int, bool, error) {
	v, ok := e.nextPath.One(doc)
	if !ok || v == nil || v == "" {
		return nil, page, false, nil
	}

	s, isString := v.(string)
	if !isString {
		return nil, page, false, fmt.Errorf("next is %s, want a URL string", typeName(v))
	}

	next, err := cur.Parse(s)
	if err != nil {
		return nil, page, false, fmt.Errorf("next %q is not a URL", s)
	}

	if next.Scheme != e.base.Scheme || next.Host != e.base.Host {
		return nil, page, false, fmt.Errorf("next points to a different host (%s)", next.Host)
	}

	if next.String() == cur.String() {
		return nil, page, false, errors.New("next is the URL just fetched")
	}

	return next, page, true, nil
}
