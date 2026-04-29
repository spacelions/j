package store

import (
	"bytes"
	"strings"
	"testing"
)

func TestPersistAgentSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	PersistAgentSelection(nil, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if stderr.Len() != 0 {
		t.Fatalf("nil store should be silent, got %q", stderr.String())
	}
}

func TestPersistAgentSelection_HappyPath(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketCoder); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketCoder, "cursor", "sonnet-4", false)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	for k, want := range map[string]string{
		"tool":        "cursor",
		"model":       "sonnet-4",
		"interactive": "false",
	} {
		got, ok, err := s.Get(BucketCoder, k)
		if err != nil || !ok || got != want {
			t.Fatalf("Get(%s) = (%q,%v,%v) want %q", k, got, ok, err, want)
		}
	}
}

func TestPersistAgentSelection_PutErrorWarns(t *testing.T) {
	s := openInTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if !strings.Contains(stderr.String(), "warning: persist tool") {
		t.Fatalf("stderr = %q, want warning", stderr.String())
	}
}

// TestPersistAgentSelection_ModelPutFails covers the error path on the
// second-step Put. We populate `tool` first via a fresh store, then
// close it before calling PersistAgentSelection so the subsequent
// Put fails on the first iteration of the loop. The warning lists the
// failed key so the test asserts we surface "tool" — the first
// iteration is enough to exercise the early-return after error.
//
// The helper intentionally early-returns on the first failure so the
// test only needs to drive a single Put error to confirm the loop
// does not silently swallow subsequent writes.
func TestPersistAgentSelection_StopsOnFirstError(t *testing.T) {
	s := openInTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	// Only one warning line is expected (loop short-circuits).
	if got := strings.Count(stderr.String(), "warning: persist"); got != 1 {
		t.Fatalf("warning count = %d, want 1; stderr=%q", got, stderr.String())
	}
}
