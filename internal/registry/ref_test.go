package registry

import "testing"

func TestIsThirdParty(t *testing.T) {
	cases := []struct {
		uses string
		want bool
	}{
		{"github.com/org/repo@v0.2.0", true},
		{"datasplice/csv@latest", false},
		{"datasplice/csv", false}, // no version at all
		{"./datasplice.yaml", false},
		{"../pkg/datasplice.yaml", false},
	}

	for _, c := range cases {
		if got := IsThirdParty(c.uses); got != c.want {
			t.Errorf("IsThirdParty(%q) = %v, want %v", c.uses, got, c.want)
		}
	}
}

func TestParseRef(t *testing.T) {
	r, err := ParseRef("github.com/myorg/datasplice-zendesk@v0.2.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	want := Ref{Host: "github.com", Path: "myorg/datasplice-zendesk", Version: "v0.2.0"}
	if r != want {
		t.Fatalf("ParseRef = %+v, want %+v", r, want)
	}

	if r.String() != "github.com/myorg/datasplice-zendesk@v0.2.0" {
		t.Fatalf("String() = %q", r.String())
	}
}

func TestParseRefRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"github.com/org/repo", "github.com/org/repo@", "noversionatall@v1"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) succeeded, want an error", bad)
		}
	}
}
