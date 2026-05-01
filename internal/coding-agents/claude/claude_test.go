package claude

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestAgent_Name(t *testing.T) {
	if got := New().Name(); got != "claude" {
		t.Fatalf("Name = %q, want %q", got, "claude")
	}
}

// TestListModels pins the static picker list. Claude has no
// `--list-models` so we expose a fixed slice; the test guards against
// accidental edits to the slice and confirms ListModels returns a
// fresh copy (callers must not be able to mutate the package state).
func TestListModels_StaticAliases(t *testing.T) {
	a := New()
	got, err := a.ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"opus", "sonnet", "haiku"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListModels = %v, want %v", got, want)
	}
	got[0] = "MUTATED"
	again, err := New().ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if again[0] == "MUTATED" {
		t.Fatalf("ListModels returned a shared slice — caller mutation leaked: %v", again)
	}
}

// TestNewResumeID_FreshUUIDs pins the local UUID-minting contract: two
// calls return distinct, parseable RFC 4122 UUIDs.
func TestNewResumeID_FreshUUIDs(t *testing.T) {
	a := New()
	a1, err := a.NewResumeID(t.Context())
	if err != nil {
		t.Fatalf("NewResumeID: %v", err)
	}
	a2, err := a.NewResumeID(t.Context())
	if err != nil {
		t.Fatalf("NewResumeID: %v", err)
	}
	if a1 == a2 {
		t.Fatalf("NewResumeID returned the same id twice: %q", a1)
	}
	if _, err := uuid.Parse(a1); err != nil {
		t.Fatalf("a1 = %q is not a valid UUID: %v", a1, err)
	}
	if _, err := uuid.Parse(a2); err != nil {
		t.Fatalf("a2 = %q is not a valid UUID: %v", a2, err)
	}
}

// TestSessionArgs pins the three branches of the session-arg builder:
// empty id => nil; resume=true => --resume; resume=false => --session-id.
func TestSessionArgs(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		resume bool
		want   []string
	}{
		{"empty", "", false, nil},
		{"empty-resume", "", true, nil},
		{"first-run", "abc-id", false, []string{"--session-id", "abc-id"}},
		{"resume", "abc-id", true, []string{"--resume", "abc-id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionArgs(tc.id, tc.resume)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sessionArgs(%q, %v) = %v, want %v", tc.id, tc.resume, got, tc.want)
			}
		})
	}
}
