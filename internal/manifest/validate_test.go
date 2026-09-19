package manifest

import "testing"

func testManifest() *Manifest {
	return &Manifest{
		Settings: map[string]SettingSpec{
			"subdomain": {Type: "string", Required: true},
			"resource":  {Type: "string", Required: true, OneOf: []string{"tickets", "users"}},
			"limit":     {Type: "number"},
		},
		Secrets: map[string]SecretSpec{
			"TOKEN": {},
		},
	}
}

func TestValidateWithRequiredMissing(t *testing.T) {
	m := testManifest()
	err := m.ValidateWith(map[string]any{"resource": "tickets"})
	if err == nil {
		t.Fatal("expected error for missing required setting")
	}
}

func TestValidateWithWrongType(t *testing.T) {
	m := testManifest()
	with := map[string]any{"subdomain": "acme", "resource": "tickets", "limit": "not a number"}
	if err := m.ValidateWith(with); err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestValidateWithOneOfRejectsOutOfSet(t *testing.T) {
	m := testManifest()
	with := map[string]any{"subdomain": "acme", "resource": "organizations"}
	if err := m.ValidateWith(with); err == nil {
		t.Fatal("expected error for value not in one_of")
	}
}

func TestValidateWithAccepts(t *testing.T) {
	m := testManifest()
	with := map[string]any{"subdomain": "acme", "resource": "tickets", "limit": 10.0}
	if err := m.ValidateWith(with); err != nil {
		t.Fatalf("ValidateWith: %v", err)
	}
}

func TestValidateSecretsCovered(t *testing.T) {
	m := testManifest()
	if err := m.ValidateSecretsCovered(nil); err == nil {
		t.Fatal("expected error: TOKEN not covered")
	}
	if err := m.ValidateSecretsCovered([]string{"TOKEN"}); err != nil {
		t.Fatalf("ValidateSecretsCovered: %v", err)
	}
}
