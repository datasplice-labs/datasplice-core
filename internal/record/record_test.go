package record

import "testing"

func TestGetSetDelete(t *testing.T) {
	r := Record{}
	r.Set("requester.name", "Ada")
	r.Set("id", 5)

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
