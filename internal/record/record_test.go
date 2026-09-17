package record

import "testing"

func TestGetSetDelete(t *testing.T) {
	r := Record{}
	if err := r.Set("requester.name", "Ada"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := r.Set("id", 5); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if v, ok := r.Get("requester.name"); !ok || v != "Ada" {
		t.Fatalf("Get(requester.name) = %v, %v", v, ok)
	}
	if v, ok := r.Get("id"); !ok || v != 5 {
		t.Fatalf("Get(id) = %v, %v", v, ok)
	}
	if _, ok := r.Get("missing.path"); ok {
		t.Fatalf("Get(missing.path) should not be found")
	}

	r.Delete("requester.name")
	if _, ok := r.Get("requester.name"); ok {
		t.Fatalf("requester.name should be deleted")
	}
	if _, ok := r.Get("requester"); !ok {
		t.Fatalf("requester map itself should still exist")
	}
}

func TestGetNesting(t *testing.T) {
	r := Record{"a": map[string]any{"b": map[string]any{"c": "deep"}}}
	if v, ok := r.Get("a.b.c"); !ok || v != "deep" {
		t.Fatalf("Get(a.b.c) = %v, %v", v, ok)
	}
	if _, ok := r.Get("a.b.c.d"); ok {
		t.Fatalf("Get past a scalar should fail")
	}
	if _, ok := r.Get("a.missing"); ok {
		t.Fatalf("Get through a missing intermediate key should fail")
	}
	if _, ok := r.Get("x.y.z"); ok {
		t.Fatalf("Get on an entirely absent path should fail")
	}
}

func TestGetOnEmptyRecord(t *testing.T) {
	r := Record{}
	if _, ok := r.Get("anything"); ok {
		t.Fatalf("Get on an empty record should fail")
	}
	if r.Has("anything") {
		t.Fatalf("Has on an empty record should be false")
	}
}

func TestUnicodeKeys(t *testing.T) {
	r := Record{}
	if err := r.Set("名前", "Ada"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := r.Set("café.☕", "cup"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := r.Get("名前"); !ok || v != "Ada" {
		t.Fatalf("Get(名前) = %v, %v", v, ok)
	}
	if v, ok := r.Get("café.☕"); !ok || v != "cup" {
		t.Fatalf("Get(café.☕) = %v, %v", v, ok)
	}
}

func TestSetThroughNonMapErrors(t *testing.T) {
	r := Record{"a": "scalar"}
	if err := r.Set("a.b", "x"); err == nil {
		t.Fatalf("Set through a non-map value should error")
	}
	// the original value must survive a rejected write
	if v, ok := r.Get("a"); !ok || v != "scalar" {
		t.Fatalf("Get(a) = %v, %v, want unchanged \"scalar\"", v, ok)
	}
}

func TestHas(t *testing.T) {
	r := Record{"a": map[string]any{"b": nil}}
	if !r.Has("a.b") {
		t.Fatalf("Has(a.b) should be true even though the value is nil")
	}
	if r.Has("a.c") {
		t.Fatalf("Has(a.c) should be false")
	}
}

func TestKeys(t *testing.T) {
	r := Record{"a": 1, "b": 2}
	keys := r.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() = %v, want 2 entries", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("Keys() = %v, want a and b", keys)
	}
}

func TestClone(t *testing.T) {
	r := Record{"a": map[string]any{"b": []any{1.0, 2.0}}}
	c := r.Clone()

	if err := c.Set("a.b", []any{99.0}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	orig, _ := r.Get("a.b")
	if got := orig.([]any); len(got) != 2 {
		t.Fatalf("mutating the clone changed the original: %v", got)
	}

	nested, _ := c.Get("a")
	nestedMap := nested.(map[string]any)
	nestedMap["injected"] = true
	if r.Has("a.injected") {
		t.Fatalf("mutating the clone's nested map changed the original")
	}
}

func TestTypedAccessors(t *testing.T) {
	r := Record{"s": "hello", "n": 42.0, "b": true}

	if v, ok := r.String("s"); !ok || v != "hello" {
		t.Fatalf("String(s) = %v, %v", v, ok)
	}
	if _, ok := r.String("n"); ok {
		t.Fatalf("String(n) should fail: n is a number, not a string")
	}
	if _, ok := r.String("missing"); ok {
		t.Fatalf("String(missing) should fail")
	}

	if v, ok := r.Number("n"); !ok || v != 42.0 {
		t.Fatalf("Number(n) = %v, %v", v, ok)
	}
	if _, ok := r.Number("s"); ok {
		t.Fatalf("Number(s) should fail: s is a string, not a number")
	}

	if v, ok := r.Bool("b"); !ok || v != true {
		t.Fatalf("Bool(b) = %v, %v", v, ok)
	}
	if _, ok := r.Bool("s"); ok {
		t.Fatalf("Bool(s) should fail: s is a string, not a bool")
	}
}
