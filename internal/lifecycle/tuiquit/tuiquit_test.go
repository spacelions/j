package tuiquit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

func TestDecidePlan_PlanPresent(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, tasks.PlanFileName)
	writeFile(t, planPath, "# Plan\n\nSome plan content.\n")

	ev, err := DecidePlan(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanDone {
		t.Errorf("DecidePlan(present, no-approval) = %q, want %q",
			ev, tasks.EventPlanDone)
	}
}

func TestDecidePlan_PlanPresentRequiresApproval(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, tasks.PlanFileName), "# Plan\n")

	ev, err := DecidePlan(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanAwaitApproval {
		t.Errorf("DecidePlan(present, approval) = %q, want %q",
			ev, tasks.EventPlanAwaitApproval)
	}
}

func TestDecidePlan_PlanMissing(t *testing.T) {
	dir := t.TempDir()
	ev, err := DecidePlan(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanQuit {
		t.Errorf("DecidePlan(missing) = %q, want %q",
			ev, tasks.EventPlanQuit)
	}
}

func TestDecidePlan_PlanEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, tasks.PlanFileName), "")

	ev, err := DecidePlan(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if ev != tasks.EventPlanQuit {
		t.Errorf("DecidePlan(empty) = %q, want %q", ev, tasks.EventPlanQuit)
	}
}

func TestDecideWork_PRLInAgentLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	writeFile(t, logPath,
		"some output\nCreated pull request https://github.com/owner/repo/pull/99\nmore output\n")

	ev, url := DecideWork(t.Context(), dir, "my-branch", logPath)
	if ev != tasks.EventWorkDone {
		t.Errorf("DecideWork = %q, want %q", ev, tasks.EventWorkDone)
	}
	if url != "https://github.com/owner/repo/pull/99" {
		t.Errorf("url = %q, want github.com/owner/repo/pull/99", url)
	}
}

func TestDecideWork_NoPR(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	writeFile(t, logPath, "just some log output, no PR here\n")

	ev, url := DecideWork(t.Context(), dir, "my-branch", logPath)
	if ev != tasks.EventWorkQuit {
		t.Errorf("DecideWork = %q, want %q", ev, tasks.EventWorkQuit)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestDecideWork_NoAgentLog(t *testing.T) {
	dir := t.TempDir()
	ev, url := DecideWork(t.Context(), dir, "my-branch",
		filepath.Join(dir, "nonexistent.log"))
	if ev != tasks.EventWorkQuit {
		t.Errorf("DecideWork = %q, want %q", ev, tasks.EventWorkQuit)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestDecideVerify_Pass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, tasks.VerifierFindingsFileName),
		"some findings\nVERDICT: PASS\n")

	ev := DecideVerify(dir)
	if ev != tasks.EventVerifyPass {
		t.Errorf("DecideVerify(PASS) = %q, want %q",
			ev, tasks.EventVerifyPass)
	}
}

func TestDecideVerify_Fail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, tasks.VerifierFindingsFileName),
		"some findings\nVERDICT: FAIL\n")

	ev := DecideVerify(dir)
	if ev != tasks.EventVerifyFail {
		t.Errorf("DecideVerify(FAIL) = %q, want %q",
			ev, tasks.EventVerifyFail)
	}
}

func TestDecideVerify_MissingFile(t *testing.T) {
	dir := t.TempDir()
	ev := DecideVerify(dir)
	if ev != tasks.EventVerifyQuit {
		t.Errorf("DecideVerify(missing) = %q, want %q",
			ev, tasks.EventVerifyQuit)
	}
}

func TestParseAgentLogForPR(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantURL string
	}{
		{
			name:    "standard",
			content: "Created https://github.com/a/b/pull/1 today",
			wantURL: "https://github.com/a/b/pull/1",
		},
		{
			name:    "no pr",
			content: "some log output",
			wantURL: "",
		},
		{
			name:    "multiple prs picks first",
			content: "PR: https://github.com/a/b/pull/1 then https://github.com/a/b/pull/2",
			wantURL: "https://github.com/a/b/pull/1",
		},
		{
			name:    "multiline picks first line with pr",
			content: "line 1\nline 2 https://github.com/x/y/pull/99\nline 3\n",
			wantURL: "https://github.com/x/y/pull/99",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAgentLogForPR(strings.NewReader(tt.content))
			if got != tt.wantURL {
				t.Errorf("parseAgentLogForPR = %q, want %q",
					got, tt.wantURL)
			}
		})
	}
}

func TestRunGhPRList_EmptyBranch(t *testing.T) {
	url := runGhPRList(t.Context(), "")
	if url != "" {
		t.Errorf("expected empty for empty branch, got %q", url)
	}
}

func TestDetectPullRequestURL_AgentLogHit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	writeFile(t, logPath,
		"opened https://github.com/o/r/pull/7\n")

	got := DetectPullRequestURL(t.Context(), "", logPath)
	if got != "https://github.com/o/r/pull/7" {
		t.Errorf("DetectPullRequestURL = %q", got)
	}
}

func TestDetectPullRequestURL_MissingLog(t *testing.T) {
	dir := t.TempDir()
	got := DetectPullRequestURL(t.Context(), "",
		filepath.Join(dir, "no.log"))
	if got != "" {
		t.Errorf("DetectPullRequestURL = %q, want empty", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
