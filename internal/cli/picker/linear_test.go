package picker

import (
	"io"
	"strings"
	"testing"
)

// TestPickLinearProject_Empty pins the empty-list short-circuit: an
// empty projects slice yields ok=false with no UI driven, mirroring
// PickTask's empty-list contract. The huh-driven branches are
// covered by the cli's end-to-end tests via the scripted SourceUI
// fakes; they are not runnable here without a TTY (Makefile
// allowlist).
func TestPickLinearProject_Empty(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	prj, ok, err := p.PickLinearProject(t.Context(), nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatalf("ok = true; want false for empty list")
	}
	if prj.ID != "" || prj.Name != "" {
		t.Fatalf("prj = %+v", prj)
	}
}
