package manifest

import "testing"

func TestResolveSettingsSubstitution(t *testing.T) {
	got, err := Resolve("https://{{ settings.subdomain }}.zendesk.com", map[string]any{"subdomain": "acme"}, nil, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "https://acme.zendesk.com" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveUnknownSettingErrors(t *testing.T) {
	if _, err := Resolve("{{ settings.missing }}", map[string]any{}, nil, false); err == nil {
		t.Fatal("expected error for unresolved setting")
	}
}

func TestResolveSecretsRequiresAllow(t *testing.T) {
	secrets := map[string]string{"TOKEN": "sk-abc"}
	if _, err := Resolve("{{ secrets.TOKEN }}", nil, secrets, false); err == nil {
		t.Fatal("expected error: secrets not allowed here")
	}
	got, err := Resolve("Bearer {{ secrets.TOKEN }}", nil, secrets, true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "Bearer sk-abc" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveUnknownSecretErrors(t *testing.T) {
	if _, err := Resolve("{{ secrets.MISSING }}", nil, map[string]string{}, true); err == nil {
		t.Fatal("expected error for unresolved secret")
	}
}
