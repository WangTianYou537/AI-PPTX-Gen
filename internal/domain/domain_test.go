package domain

import "testing"

func TestClampSlideConcurrency(t *testing.T) {
	if got := ClampSlideConcurrency(0, 5); got != 2 {
		t.Fatalf("expected default 2, got %d", got)
	}
	if got := ClampSlideConcurrency(99, 3); got != 3 {
		t.Fatalf("expected min(max, slides)=3, got %d", got)
	}
	if got := ClampSlideConcurrency(4, 0); got != 4 {
		t.Fatalf("expected 4, got %d", got)
	}
}

func TestResolveSlideConcurrencyPriority(t *testing.T) {
	userLimit := 5
	limit, source := ResolveSlideConcurrency(&userLimit, 3, 2, 10)
	if limit != 5 || source != "user" {
		t.Fatalf("user override failed: limit=%d source=%s", limit, source)
	}
	limit, source = ResolveSlideConcurrency(nil, 3, 2, 10)
	if limit != 3 || source != "group" {
		t.Fatalf("group priority failed: limit=%d source=%s", limit, source)
	}
	limit, source = ResolveSlideConcurrency(nil, 0, 2, 10)
	if limit != 2 || source != "system" {
		t.Fatalf("system fallback failed: limit=%d source=%s", limit, source)
	}
}

func TestParseRequestJSON(t *testing.T) {
	got, err := ParseRequestJSON(`{"temperature":0.2}`)
	if err != nil {
		t.Fatal(err)
	}
	if got["temperature"] != 0.2 {
		t.Fatalf("unexpected payload: %#v", got)
	}
	if _, err := ParseRequestJSON(`[]`); err == nil {
		t.Fatal("expected array to fail")
	}
}
