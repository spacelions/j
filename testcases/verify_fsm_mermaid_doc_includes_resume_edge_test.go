package testcases_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVerify_FSMMermaidDoc_IncludesPendingApprovalResumeEdge pins
// acceptance criterion: the package-level Mermaid state diagram in
// fsm.go must list the new edge so the human-readable doc stays in
// sync with the transitions table.
func TestVerify_FSMMermaidDoc_IncludesPendingApprovalResumeEdge(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	fsmPath := filepath.Join(
		repoRoot, "internal", "store", "tasks", "fsm.go",
	)
	body, err := os.ReadFile(fsmPath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", fsmPath, err)
	}
	want := "plan-pending-approval --> planning : EventPlanResume"
	if !strings.Contains(string(body), want) {
		t.Fatalf(
			"fsm.go Mermaid doc missing edge %q; "+
				"the comment must list the new transition",
			want,
		)
	}
}
