package verifier

import (
	"testing"
)

func TestConstants(t *testing.T) {
	if Name != "verifier" {
		t.Fatalf("Name = %q", Name)
	}
	if OutputKey != "temp:review" {
		t.Fatalf("OutputKey = %q", OutputKey)
	}
	if instruction == "" {
		t.Fatal("instruction is empty")
	}
}

func TestNew_Success(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a == nil {
		t.Fatal("agent is nil")
	}
	if a.Name() != Name {
		t.Fatalf("Name() = %q", a.Name())
	}
}
