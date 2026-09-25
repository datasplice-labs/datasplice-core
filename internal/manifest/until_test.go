package manifest

import "testing"

func TestParseUntil(t *testing.T) {
	cases := []struct {
		in      string
		kind    UntilKind
		path    string
		equal   bool
		value   any
		wantErr bool
	}{
		{in: "", kind: UntilNone},
		{in: "empty", kind: UntilEmpty},
		{in: "$.meta.has_more == false", kind: UntilCompare, path: "$.meta.has_more", equal: true, value: false},
		{in: "$.next != null", kind: UntilCompare, path: "$.next", equal: false, value: nil},
		{in: `$.status == "done"`, kind: UntilCompare, path: "$.status", equal: true, value: "done"},
		{in: "$.total == 3", kind: UntilCompare, path: "$.total", equal: true, value: 3.0},
		{in: "$.a == 1 && $.b == 2", wantErr: true},
		{in: "$.a > 1", wantErr: true},
		{in: "$.a == maybe", wantErr: true},
		{in: "whatever", wantErr: true},
	}

	for _, c := range cases {
		got, err := ParseUntil(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseUntil(%q): err = %v, wantErr = %v", c.in, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if got.Kind != c.kind || got.Path != c.path || got.Equal != c.equal || got.Value != c.value {
			t.Errorf("ParseUntil(%q) = %+v", c.in, got)
		}
	}
}
