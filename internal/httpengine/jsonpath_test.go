package httpengine

import (
	"encoding/json"
	"reflect"
	"testing"
)

const sampleDoc = `{
  "tickets": [{"id": 1, "custom_fields": [{"id": 360001, "value": "billing"}, {"id": 5, "value": "x"}]}],
  "data": {"items": [1, 2]},
  "content": [{"text": "hello"}]
}`

func mustDecode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// T5.6: the three paths the spec names, plus the filter form.
func TestPathOne(t *testing.T) {
	doc := mustDecode(t, sampleDoc)
	ticket := doc.(map[string]any)["tickets"].([]any)[0]

	cases := []struct {
		path string
		on   any
		want any
		ok   bool
	}{
		{"$.data.items", doc, []any{1.0, 2.0}, true},
		{"$.content[0].text", doc, "hello", true},
		{"$.custom_fields[?(@.id==360001)].value", ticket, "billing", true},
		{"$.tickets[*].id", doc, 1.0, true},
		{"$.missing", doc, nil, false},
	}

	for _, c := range cases {
		p, err := CompilePath(c.path)
		if err != nil {
			t.Fatalf("CompilePath(%q): %v", c.path, err)
		}
		got, ok := p.One(c.on)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, %v; want %#v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestCompilePathRejectsBadSyntax(t *testing.T) {
	for _, bad := range []string{"tickets", "$.a[", "$.{{ x }}"} {
		if _, err := CompilePath(bad); err == nil {
			t.Errorf("CompilePath(%q) succeeded, want an error", bad)
		}
	}
}
