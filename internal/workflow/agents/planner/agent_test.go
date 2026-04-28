package planner

import (
	"testing"
)

func TestConstants(t *testing.T) {
	if Name != "planner" {
		t.Fatalf("Name = %q", Name)
	}
	if OutputKey != "plan" {
		t.Fatalf("OutputKey = %q", OutputKey)
	}
	if Instruction == "" {
		t.Fatal("Instruction is empty")
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
