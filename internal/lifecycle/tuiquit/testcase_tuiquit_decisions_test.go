package tuiquit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/lifecycle/tuiquit"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCase_DecidePlan_ArtifactPresent_Success verifies that a non-empty
// plan.md produces EventPlanDone when approval is not required.
func TestCase_DecidePlan_ArtifactPresent_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, tasks.PlanFileName),
		[]byte("# Plan content"), 0o644)

	ev, err := tuiquit.DecidePlan(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanDone {
		t.Fatalf("event = %q, want %q", ev, tasks.EventPlanDone)
	}
}

// TestCase_DecidePlan_ApprovalRequired verifies plan.md + approval=true
// yields EventPlanAwaitApproval.
func TestCase_DecidePlan_ApprovalRequired(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, tasks.PlanFileName),
		[]byte("# Plan"), 0o644)

	ev, err := tuiquit.DecidePlan(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanAwaitApproval {
		t.Fatalf("event = %q, want %q", ev, tasks.EventPlanAwaitApproval)
	}
}

// TestCase_DecidePlan_ArtifactMissing signals TUI quit when plan.md absent.
func TestCase_DecidePlan_ArtifactMissing(t *testing.T) {
	ev, err := tuiquit.DecidePlan(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanQuit {
		t.Fatalf("event = %q, want %q", ev, tasks.EventPlanQuit)
	}
}

// TestCase_DecideWork_URLInAgentLog detects a GitHub PR URL in agent.log.
func TestCase_DecideWork_URLInAgentLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	os.WriteFile(logPath,
		[]byte("Created https://github.com/org/repo/pull/99\n"), 0o644)

	ev, url := tuiquit.DecideWork(t.Context(), dir, "branch", logPath)
	if ev != tasks.EventWorkDone {
		t.Fatalf("event = %q, want %q", ev, tasks.EventWorkDone)
	}
	if url != "https://github.com/org/repo/pull/99" {
		t.Fatalf("url = %q", url)
	}
}

// TestCase_DecideWork_NoPR detects TUI quit when no PR URL is found.
func TestCase_DecideWork_NoPR(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	os.WriteFile(logPath, []byte("no PR here\n"), 0o644)

	ev, url := tuiquit.DecideWork(t.Context(), dir, "branch", logPath)
	if ev != tasks.EventWorkQuit {
		t.Fatalf("event = %q, want %q", ev, tasks.EventWorkQuit)
	}
	if url != "" {
		t.Fatalf("url = %q, want empty", url)
	}
}

// TestCase_DecideVerify_Pass detects VERDICT: PASS in findings file.
func TestCase_DecideVerify_Pass(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, tasks.VerifierFindingsFileName),
		[]byte("findings\nVERDICT: PASS\n"), 0o644)

	ev := tuiquit.DecideVerify(dir)
	if ev != tasks.EventVerifyPass {
		t.Fatalf("event = %q, want %q", ev, tasks.EventVerifyPass)
	}
}

// TestCase_DecideVerify_Fail detects VERDICT: FAIL in findings file.
func TestCase_DecideVerify_Fail(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, tasks.VerifierFindingsFileName),
		[]byte("issues\nVERDICT: FAIL\n"), 0o644)

	ev := tuiquit.DecideVerify(dir)
	if ev != tasks.EventVerifyFail {
		t.Fatalf("event = %q, want %q", ev, tasks.EventVerifyFail)
	}
}

// TestCase_DecideVerify_MissingFile detects TUI quit when file absent.
func TestCase_DecideVerify_MissingFile(t *testing.T) {
	ev := tuiquit.DecideVerify(t.TempDir())
	if ev != tasks.EventVerifyQuit {
		t.Fatalf("event = %q, want %q", ev, tasks.EventVerifyQuit)
	}
}

