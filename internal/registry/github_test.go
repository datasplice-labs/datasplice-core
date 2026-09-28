package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHub stands in for both api.github.com and raw.githubusercontent.com.
// repos maps "owner/repo" -> tag names; manifests maps "owner/repo@tag" ->
// file bytes (absent means 404, same as a repo/tag with no datasplice.yaml).
type fakeGitHub struct {
	repos     map[string][]string
	manifests map[string][]byte
}

func (g *fakeGitHub) api() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		switch {
		case strings.HasSuffix(path, "/tags"):
			repo := strings.TrimSuffix(path, "/tags")
			tags, ok := g.repos[repo]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			names := make([]string, len(tags))
			for i, t := range tags {
				names[i] = `{"name":"` + t + `"}`
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte("[" + strings.Join(names, ",") + "]")); err != nil {
				panic(err)
			}
		default:
			if _, ok := g.repos[path]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			if _, err := w.Write([]byte(`{}`)); err != nil {
				panic(err)
			}
		}
	}))
}

// raw serves /owner/repo/tag/datasplice.yaml, keyed in g.manifests as
// "owner/repo@tag" — the same shape Fetch requests
// (rawBase + ref.Path + "/" + ref.Version + "/datasplice.yaml").
func (g *fakeGitHub) raw() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/datasplice.yaml")

		i := strings.LastIndex(path, "/")
		if i < 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		data, ok := g.manifests[path[:i]+"@"+path[i+1:]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if _, err := w.Write(data); err != nil {
			panic(err)
		}
	}))
}

func newFetcher(t *testing.T, g *fakeGitHub) *Fetcher {
	t.Helper()
	api := g.api()
	t.Cleanup(api.Close)
	raw := g.raw()
	t.Cleanup(raw.Close)

	return &Fetcher{APIBase: api.URL, RawBase: raw.URL}
}

func TestFetchSucceeds(t *testing.T) {
	g := &fakeGitHub{
		repos:     map[string][]string{"myorg/pkg": {"v0.1.0", "v0.2.0"}},
		manifests: map[string][]byte{"myorg/pkg@v0.2.0": []byte("name: pkg\n")},
	}
	f := newFetcher(t, g)

	data, err := f.Fetch(context.Background(), Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(data) != "name: pkg\n" {
		t.Fatalf("data = %q", data)
	}
}

// T6.9: repo missing, tag missing, and manifest missing must be
// distinguishable, and the tag case must list available tags.
func TestFetchDistinguishesFailures(t *testing.T) {
	g := &fakeGitHub{
		repos:     map[string][]string{"myorg/pkg": {"v0.1.0", "v0.2.0"}, "myorg/empty": {}},
		manifests: map[string][]byte{}, // pkg@v0.2.0 deliberately has no manifest
	}
	f := newFetcher(t, g)

	cases := []struct {
		name string
		ref  Ref
		want string
	}{
		{"repo not found", Ref{Host: "github.com", Path: "myorg/nope", Version: "v1.0.0"}, "repository not found"},
		{"no tags at all", Ref{Host: "github.com", Path: "myorg/empty", Version: "v1.0.0"}, "no tags"},
		{"tag not found, lists available", Ref{Host: "github.com", Path: "myorg/pkg", Version: "v9.9.9"}, "available: v0.1.0, v0.2.0"},
		{"no manifest at tag", Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}, "no datasplice.yaml"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.Fetch(context.Background(), c.ref)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestFetchRejectsLatestForThirdParty(t *testing.T) {
	f := &Fetcher{}
	_, err := f.Fetch(context.Background(), Ref{Host: "github.com", Path: "myorg/pkg", Version: "latest"})
	if err == nil || !strings.Contains(err.Error(), "@latest") {
		t.Fatalf("err = %v, want it to reject @latest", err)
	}
}

func TestFetchRejectsNonGitHubHost(t *testing.T) {
	f := &Fetcher{}
	_, err := f.Fetch(context.Background(), Ref{Host: "gitlab.com", Path: "org/pkg", Version: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("err = %v, want it to name github.com as the only supported host", err)
	}
}
