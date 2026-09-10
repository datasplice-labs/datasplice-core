package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// HTTP is a source builtin: fetches JSON, walks with.records_path (a
// dotted path, like record.Get) to find the array of records, and
// optionally paginates and rate-limits.
//
// ponytail: pagination supports "cursor" (a response field holding the
// next URL) and "page" (increment a query param) — link-header and
// offset styles aren't implemented; add them the same way when a real
// source needs one (datasplice-core-prd.md M1).
type HTTP struct {
	url         string
	method      string
	headers     map[string]string
	basicUser   string
	basicPass   string
	recordsPath string
	pagination  map[string]any
	minInterval time.Duration
}

func NewHTTP() *HTTP { return &HTTP{} }

func (h *HTTP) Describe() contract.Describe {
	return contract.Describe{
		Name: "http", Version: "0.1.0", Role: contract.RoleSource,
		Settings: []contract.SettingSpec{{Key: "url", Type: "string", Required: true}},
	}
}

func (h *HTTP) Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error {
	u, ok := settings["url"].(string)
	if !ok {
		return fmt.Errorf("http: `with.url` must be a string")
	}

	if u == "" {
		return fmt.Errorf("http: `with.url` is required")
	}
	h.url = u

	h.method, _ = settings["method"].(string)
	// Default to GET if not specified, since most sources will be GET.
	if h.method == "" {
		h.method = http.MethodGet
	}

	h.headers = map[string]string{}
	if hdrs, ok := settings["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			h.headers[k] = fmt.Sprint(v)
		}
	}

	if auth, ok := settings["auth"].(map[string]any); ok {
		if err := h.configureAuth(auth); err != nil {
			return err
		}
	}

	h.recordsPath, _ = settings["records_path"].(string)
	if p, ok := settings["pagination"].(map[string]any); ok {
		h.pagination = p
	}

	if rl, ok := settings["rate_limit"].(float64); ok && rl > 0 {
		h.minInterval = time.Duration(float64(time.Second) / rl)
	}

	return nil
}

func (h *HTTP) configureAuth(auth map[string]any) error {
	typ, _ := auth["type"].(string)
	switch typ {
	case "", "none":
	case "bearer":
		token, _ := auth["token"].(string)
		h.headers["Authorization"] = "Bearer " + token
	case "basic":
		h.basicUser, _ = auth["username"].(string)
		h.basicPass, _ = auth["password"].(string)
	case "api_token":
		header, _ := auth["header"].(string)
		if header == "" {
			header = "Authorization"
		}
		token, _ := auth["token"].(string)
		h.headers[header] = token
	default:
		return fmt.Errorf("http: unknown auth.type %q", typ)
	}

	return nil
}

func (h *HTTP) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	reqURL := h.url
	var lastReq time.Time
	page := 1
	for reqURL != "" {
		if h.minInterval > 0 && !lastReq.IsZero() {
			if wait := h.minInterval - time.Since(lastReq); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		body, err := h.fetch(ctx, reqURL)
		if err != nil {
			return fmt.Errorf("http: %w", err)
		}
		lastReq = time.Now()

		var doc any
		if err := json.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("http: decoding response: %w", err)
		}
		root, _ := doc.(map[string]any)

		items := extractRecords(root, doc, h.recordsPath)
		if len(items) > 0 {
			select {
			case out <- items:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		reqURL, page, err = h.nextURL(root, reqURL, page, len(items) > 0)
		if err != nil {
			return fmt.Errorf("http: %w", err)
		}
	}
	return nil
}

func (h *HTTP) fetch(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, h.method, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	if h.basicUser != "" {
		req.SetBasicAuth(h.basicUser, h.basicPass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: %d: %s", target, resp.StatusCode, body)
	}
	return body, nil
}

func extractRecords(root map[string]any, doc any, path string) contract.Batch {
	target := doc
	if path != "" && root != nil {
		if v, ok := record.Record(root).Get(path); ok {
			target = v
		}
	}
	arr, _ := target.([]any)
	items := make(contract.Batch, 0, len(arr))
	for _, el := range arr {
		if m, ok := el.(map[string]any); ok {
			items = append(items, record.Record(m))
		}
	}
	return items
}

// nextURL applies with.pagination. type "cursor" reads a field naming the
// next page's URL (empty/missing = done); type "page" increments a query
// param up to max_pages, stopping early if a page came back empty.
func (h *HTTP) nextURL(root map[string]any, curURL string, page int, gotItems bool) (string, int, error) {
	if h.pagination == nil {
		return "", page, nil
	}
	typ, _ := h.pagination["type"].(string)
	switch typ {
	case "", "none":
		return "", page, nil
	case "cursor":
		field, _ := h.pagination["field"].(string)
		if field == "" {
			return "", page, fmt.Errorf("pagination.field is required for type cursor")
		}
		if root == nil {
			return "", page, nil
		}
		next, ok := record.Record(root).Get(field)
		if !ok {
			return "", page, nil
		}
		s, _ := next.(string)
		return s, page, nil
	case "page":
		if !gotItems {
			return "", page, nil
		}
		maxPages, _ := h.pagination["max_pages"].(float64)
		param, _ := h.pagination["param"].(string)
		if param == "" {
			param = "page"
		}
		page++
		if maxPages > 0 && page > int(maxPages) {
			return "", page, nil
		}
		u, err := url.Parse(curURL)
		if err != nil {
			return "", page, err
		}
		q := u.Query()
		q.Set(param, strconv.Itoa(page))
		u.RawQuery = q.Encode()
		return u.String(), page, nil
	default:
		return "", page, fmt.Errorf("unknown pagination.type %q", typ)
	}
}
