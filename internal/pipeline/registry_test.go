package pipeline

import "testing"

func TestResolveBuiltin(t *testing.T) {
	cases := []struct {
		uses    string
		wantErr bool
	}{
		{"datasplice/csv", false},        // no version: matches the docs' own main.yaml example
		{"datasplice/csv@latest", false}, // explicit @latest also accepted
		{"datasplice/csv@v1.0.0", true},  // built-ins are pinned to the core's version, not independently versioned
		{"github.com/x/y@v0.2.0", true},  // third-party resolution isn't implemented yet
		{"not-a-real-package", true},
	}

	for _, c := range cases {
		_, err := resolve(c.uses)
		if (err != nil) != c.wantErr {
			t.Errorf("resolve(%q): err = %v, wantErr = %v", c.uses, err, c.wantErr)
		}
	}
}
