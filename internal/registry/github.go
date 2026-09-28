package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const (
	defaultAPIBase = "https://api.github.com"
	defaultRawBase = "https://raw.githubusercontent.com"

	// GitHub's tags API returns 30 per page by default, 100 at most;
	// a repo with more tags than this won't have all of them
	// listed in a "no such tag" error.
	//
	// Add pagination if a real package repo ever needs it.
	tagsPerPage = 100
)

// Fetcher downloads a third-party manifest from its git host. The zero
// value talks to the real GitHub; tests override Client/APIBase/RawBase
// to point at an httptest server instead.
type Fetcher struct {
	Client  *http.Client
	APIBase string
	RawBase string
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}

	return http.DefaultClient
}

func (f *Fetcher) apiBase() string {
	if f.APIBase != "" {
		return f.APIBase
	}

	return defaultAPIBase
}

func (f *Fetcher) rawBase() string {
	if f.RawBase != "" {
		return f.RawBase
	}

	return defaultRawBase
}

// Fetch downloads ref's datasplice.yaml, checking the repo and the tag
// exist first — a bare raw-file request 404s identically whether the
// repo doesn't exist, the tag doesn't exist, or the tag exists but has
// no manifest at its root, and T6.9 wants those told apart.
func (f *Fetcher) Fetch(ctx context.Context, ref Ref) ([]byte, error) {
	if ref.Version == "latest" {
		return nil, fmt.Errorf("%s: third-party packages must pin an exact version, not @latest", ref)
	}

	if ref.Host != "github.com" {
		return nil, fmt.Errorf("%s: only github.com packages are supported right now", ref.Host)
	}

	if err := f.checkRepo(ctx, ref); err != nil {
		return nil, err
	}

	if err := f.checkTag(ctx, ref); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/%s/datasplice.yaml", f.rawBase(), ref.Path, ref.Version)
	body, status, err := f.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}

	switch status {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s: no datasplice.yaml at this tag", ref)
	default:
		return nil, fmt.Errorf("%s: fetching manifest: unexpected status %d", ref, status)
	}
}

func (f *Fetcher) checkRepo(ctx context.Context, ref Ref) error {
	url := fmt.Sprintf("%s/repos/%s", f.apiBase(), ref.Path)

	_, status, err := f.get(ctx, url)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}

	switch status {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("%s: repository not found (or private)", ref)
	default:
		return fmt.Errorf("%s: checking repository: unexpected status %d", ref, status)
	}
}

func (f *Fetcher) checkTag(ctx context.Context, ref Ref) error {
	url := fmt.Sprintf("%s/repos/%s/tags?per_page=%d", f.apiBase(), ref.Path, tagsPerPage)

	body, status, err := f.get(ctx, url)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}

	if status != http.StatusOK {
		return fmt.Errorf("%s: checking tags: unexpected status %d", ref, status)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return fmt.Errorf("%s: checking tags: %w", ref, err)
	}

	names := make([]string, 0, len(tags))
	for _, t := range tags {
		if t.Name == ref.Version {
			return nil
		}

		names = append(names, t.Name)
	}

	if len(names) == 0 {
		return fmt.Errorf("%s: repository has no tags", ref)
	}

	sort.Strings(names)

	return fmt.Errorf("%s: no tag %q (available: %s)", ref, ref.Version, strings.Join(names, ", "))
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "datasplice-core")

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	return body, resp.StatusCode, nil
}
