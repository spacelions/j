package linear

import (
	"errors"
	"testing"
)

func TestOpenURL_StubReplaceable(t *testing.T) {
	prev := OpenURL
	t.Cleanup(func() { OpenURL = prev })
	called := ""
	OpenURL = func(url string) error {
		called = url
		return nil
	}
	if err := OpenURL("https://example/x"); err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if called != "https://example/x" {
		t.Fatalf("called = %q, want stubbed argument", called)
	}
}

func TestOpenURL_StubError(t *testing.T) {
	prev := OpenURL
	t.Cleanup(func() { OpenURL = prev })
	want := errors.New("boom")
	OpenURL = func(string) error { return want }
	if err := OpenURL("https://x"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
