package config

import "testing"

func TestInterpolateResolvesAndErrors(t *testing.T) {
	with := map[string]any{
		"email": "${ZENDESK_EMAIL}",
		"auth": map[string]any{
			"token": "${ZENDESK_TOKEN}",
		},
	}
	values := map[string]string{"ZENDESK_EMAIL": "a@b.com", "ZENDESK_TOKEN": "secret"}

	out, err := Interpolate(with, values)
	if err != nil {
		t.Fatalf("Interpolate: %v", err)
	}
	if out["email"] != "a@b.com" {
		t.Fatalf("email = %v", out["email"])
	}
	auth := out["auth"].(map[string]any)
	if auth["token"] != "secret" {
		t.Fatalf("auth.token = %v", auth["token"])
	}

	if _, err := Interpolate(map[string]any{"x": "${MISSING}"}, values); err == nil {
		t.Fatalf("expected error for unresolved ${MISSING}")
	}
}

func TestSecretsResolveByType(t *testing.T) {
	plain := &Secrets{Type: EnvVarsTypePlain, Values: map[string]string{"TOK": "abc"}}
	got, err := plain.Resolve([]string{"TOK"})
	if err != nil || got["TOK"] != "abc" {
		t.Fatalf("plain.Resolve = %v, %v", got, err)
	}

	noenv := &Secrets{Type: EnvVarsTypeNoEnv}
	if _, err := noenv.Resolve([]string{"TOK"}); err == nil {
		t.Fatalf("noenv with referenced names should error")
	}
	if got, err := noenv.Resolve(nil); err != nil || len(got) != 0 {
		t.Fatalf("noenv with no referenced names should succeed empty: %v, %v", got, err)
	}

	if _, err := plain.Resolve([]string{"NOT_DECLARED"}); err == nil {
		t.Fatalf("expected error for undeclared name")
	}
}
