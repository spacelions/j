package testcases_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVerify_FSMMermaidDoc_IncludesRecoveryEdges pins the diagram
// half of the scope: the package doc-comment Mermaid diagram in
// fsm.go must list every one of the nine new edges so the human
// view stays in sync with the transitions table.
func TestVerify_FSMMermaidDoc_IncludesRecoveryEdges(t *testing.T) {
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
	doc := string(body)
	wants := []string{
		"failed --> planning : EventPlanResume",
		"failed --> working : EventWorkResume",
		"failed --> verifying : EventVerifyResume",
		"completed --> planning : EventPlanRestart",
		"completed --> planning : EventPlanResume",
		"completed --> working : EventWorkRestart",
		"completed --> working : EventWorkResume",
		"completed --> verifying : EventVerifyRestart",
		"completed --> verifying : EventVerifyResume",
	}
	for _, want := range wants {
		if !strings.Contains(doc, want) {
			t.Errorf(
				"fsm.go Mermaid doc missing edge %q", want)
		}
	}
}
