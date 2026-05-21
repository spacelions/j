package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
	"github.com/spacelions/j/internal/tools/linear"
)

// writeStartFile writes a markdown task description into the test's
// temp dir and returns its absolute path. Used by the --from-file
// happy path; the source picker tests prefer writeStartFileInCwd
// because mdfile.ListInDir scans the current working directory.
func writeStartFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeStartFileInCwd writes a markdown task description into the
// current working directory under name and returns its basename.
// Used by the source-picker tests so mdfile.ListInDir surfaces the
// file when RunStart drives pickMarkdownTarget.
func writeStartFileInCwd(t *testing.T, name, body string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// noopJBinary writes a tiny shell script that exits zero into the
// test's temp dir and returns its absolute path.
func noopJBinary(t *testing.T) string {
	t.Helper()
	return testutil.NoopJBinary(t)
}

// argvJBinary writes a tiny shell script that records its argv, one
// argument per line. The script writes to a sibling `.tmp` file and
// renames it into place so the polling reader never sees a partial
// argv list (printf '%s\n' "$@" issues one write per argument and
// the reader can otherwise race the writer between args). RunStart
// spawns it detached, so tests poll the output file after RunStart
// returns.
func argvJBinary(t *testing.T, outputPath string) string {
	t.Helper()
	return testutil.ArgvJBinary(t, outputPath)
}

func readSpawnedArgv(t *testing.T, path string) []string {
	t.Helper()
	return testutil.ReadSpawnedArgv(t, path)
}

// readTaskFromBolt opens the per-project tasks DB and returns the
// task row for id (or fails the test if missing).
func readTaskFromBolt(t *testing.T, id string) tasks.Task {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return got
}

func putProjectPlanRequiresApproval(t *testing.T, value string) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(store.BucketProject, store.KeyPlanRequiresApproval, value); err != nil {
		t.Fatalf("Put plan_requires_approval: %v", err)
	}
}

// firstSeededTaskID lists every task in the bbolt store and returns
// the only id (failing the test if the count is not exactly one).
func firstSeededTaskID(t *testing.T) string {
	t.Helper()
	rows := allTaskRows(t)
	if len(rows) != 1 {
		t.Fatalf("ListTasks = %d rows, want 1: %+v", len(rows), rows)
	}
	return rows[0].ID
}

// allTaskRows returns every row in the bbolt store; helper for the
// source-picker tests that need to assert "no new row created."
func allTaskRows(t *testing.T) []tasks.Task {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	rows, err := s.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// scriptedStartUI is the in-package fake satisfying StartUI. Each
// method returns a configured value (or error) and records call
// counts so tests can assert which branch fired.
type scriptedStartUI struct {
	source             picker.Source
	sourceErr          error
	sourceCalls        int
	pickedMarkdownPath string
	pickedMarkdownErr  error
	markdownCalls      int
	pickedTaskID       string
	pickedTaskOK       bool
	taskErr            error
	taskCalls          int

	linearAPIKey       string
	linearAPIKeyOK     bool
	linearAPIKeyErr    error
	linearAPIKeyURL    string
	linearAPIKeyCalls  int
	linearProject      linear.Project
	linearProjectOK    bool
	linearProjectErr   error
	linearProjectCalls int
	linearProjectsSeen []linear.Project
	pickedIssue        linear.Issue
	pickedIssueOK      bool
	pickedIssueErr     error
	pickedIssueCalls   int
	pickedIssuesSeen   []linear.Issue

	confirmOverride      bool
	confirmOverrideErr   error
	confirmOverrideCalls int
}

func (u *scriptedStartUI) SelectSource(_ context.Context, _ []picker.Source) (picker.Source, error) {
	u.sourceCalls++
	return u.source, u.sourceErr
}

func (u *scriptedStartUI) PickMarkdownInCwd(_ context.Context) (string, error) {
	u.markdownCalls++
	if u.pickedMarkdownErr != nil {
		return "", u.pickedMarkdownErr
	}
	return u.pickedMarkdownPath, nil
}

func (u *scriptedStartUI) PickTask(_ context.Context, _ string, _ []tasks.Task) (string, bool, error) {
	u.taskCalls++
	if u.taskErr != nil {
		return "", false, u.taskErr
	}
	return u.pickedTaskID, u.pickedTaskOK, nil
}

func (u *scriptedStartUI) PromptLinearAPIKey(_ context.Context, openURL string) (string, bool, error) {
	u.linearAPIKeyCalls++
	u.linearAPIKeyURL = openURL
	if u.linearAPIKeyErr != nil {
		return "", false, u.linearAPIKeyErr
	}
	return u.linearAPIKey, u.linearAPIKeyOK, nil
}

func (u *scriptedStartUI) PickLinearProject(_ context.Context, projects []linear.Project) (linear.Project, bool, error) {
	u.linearProjectCalls++
	u.linearProjectsSeen = append([]linear.Project(nil), projects...)
	if u.linearProjectErr != nil {
		return linear.Project{}, false, u.linearProjectErr
	}
	return u.linearProject, u.linearProjectOK, nil
}

func (u *scriptedStartUI) PickLinearIssue(_ context.Context, issues []linear.Issue) (linear.Issue, bool, error) {
	u.pickedIssueCalls++
	u.pickedIssuesSeen = append([]linear.Issue(nil), issues...)
	if u.pickedIssueErr != nil {
		return linear.Issue{}, false, u.pickedIssueErr
	}
	return u.pickedIssue, u.pickedIssueOK, nil
}

func (u *scriptedStartUI) ConfirmStatusOverride(_ context.Context, _, _, _ string) (bool, error) {
	u.confirmOverrideCalls++
	return u.confirmOverride, u.confirmOverrideErr
}

// TestRunStart_HappyPath_FromFile pins the --from-file shortcut.
func TestRunStart_HappyPath_FromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody line")
	stub := testutil.NewScriptedAgent()
	binary := noopJBinary(t)
	var stdout, stderr bytes.Buffer

	start := time.Now()
	err := RunStart(t.Context(), StartOptions{
		FromFile:    target,
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Agents:      []codingagents.Agent{stub},
		UI:          &scriptedStartUI{},
		JBinary:     binary,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("RunStart took %v, want <2s for the detached spawn", elapsed)
	}
	if !strings.Contains(stdout.String(), "running in background (PID=") {
		t.Fatalf("stdout should announce the task PID: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "tail -f .j/tasks/") || !strings.Contains(stdout.String(), "/agent.log") {
		t.Fatalf("stdout should print `tail -f .j/tasks/<id>/agent.log`: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "┌") || !strings.Contains(stdout.String(), "└") {
		t.Fatalf("stdout should be wrapped in a normal-border box (┌/└): %q", stdout.String())
	}

	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", row.Status)
	}
	wantLog := filepath.Join(".j/tasks", id, tasks.AgentLogFileName)
	if !strings.HasSuffix(row.AgentLogPath, wantLog) {
		t.Fatalf("AgentLogPath = %q, want suffix %q", row.AgentLogPath, wantLog)
	}
	if row.Summary == "" {
		t.Fatalf("Summary should be derived from the markdown body")
	}

	tasksDir := tasks.DefaultDir()
	reqPath := filepath.Join(tasksDir, id, tasks.RequirementsFileName)
	body, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("read requirements.md: %v", err)
	}
	if !strings.Contains(string(body), "body line") {
		t.Fatalf("requirements.md missing user body: %q", body)
	}
}

func TestRunStart_ForwardsResolvedPlanApproval(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }
	tests := []struct {
		name     string
		setting  string
		override *bool
		want     string
	}{
		{name: "inherits_true_setting", setting: "true", want: "true"},
		{name: "explicit_false_overrides_true", setting: "true", override: boolPtr(false), want: "false"},
		{name: "explicit_true_overrides_false", setting: "false", override: boolPtr(true), want: "true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			putProjectPlanRequiresApproval(t, tc.setting)
			for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
				testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
			}
			target := writeStartFile(t, "# task\nbody")
			argvPath := filepath.Join(t.TempDir(), "argv.txt")
			if err := RunStart(t.Context(), StartOptions{
				FromFile:             target,
				PlanRequiresApproval: tc.override,
				Stdin:                strings.NewReader(""),
				Stdout:               io.Discard,
				Stderr:               io.Discard,
				Agents:               []codingagents.Agent{testutil.NewScriptedAgent()},

				UI:      &scriptedStartUI{},
				JBinary: argvJBinary(t, argvPath),
			}); err != nil {
				t.Fatalf("RunStart: %v", err)
			}
			args := readSpawnedArgv(t, argvPath)
			if len(args) < 5 {
				t.Fatalf("argv = %v, want orchestrate args plus plan approval flag", args)
			}
			// --interactive=true is always appended after the approval flag
			wantApproval := "--plan-requires-approval=" + tc.want
			found := slices.Contains(args, wantApproval)
			if !found {
				t.Fatalf("approval flag %q not found in argv=%v", wantApproval, args)
			}
		})
	}
}

// TestRunStart_PrePopulatedSkipsPrompts pins that buckets already
// populated short-circuit the agent-pick prompts.
func TestRunStart_PrePopulatedSkipsPrompts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	binary := noopJBinary(t)

	err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		FromFile:    target,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:          &scriptedStartUI{},
		JBinary:     binary,
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
}

// TestRunStart_NoAgents pins the no-agents-configured branch.
func TestRunStart_NoAgents(t *testing.T) {
	err := RunStart(t.Context(), StartOptions{
		FromFile: "ignored",
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunStart_NoFromFile_PicksMarkdown drives the source picker
// happy path: empty FromFile + UI.SelectSource returns
// SourceMarkdown + UI.PickFromFile returns the staged .md basename.
// A new task row should be seeded just like the --from-file path.
func TestRunStart_NoFromFile_PicksMarkdown(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	writeStartFileInCwd(t, "spec.md", "# task\nbody line")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	ui := &scriptedStartUI{
		source:             picker.SourceMarkdown,
		pickedMarkdownPath: filepath.Join(cwd, "spec.md"),
	}

	err = RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if ui.sourceCalls != 1 {
		t.Fatalf("SelectSource calls = %d, want 1", ui.sourceCalls)
	}
	if ui.markdownCalls != 1 {
		t.Fatalf("PickMarkdownInCwd calls = %d, want 1", ui.markdownCalls)
	}
	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", row.Status)
	}
}

// TestRunStart_NoFromFile_PicksTask drives the re-plan branch:
// pre-seed a task in bbolt; UI.SelectSource returns SourceTask;
// UI.PickReplanTask returns the existing task's ID. RunStart must
// NOT mint a new task and must update the existing row's AgentLogPath
// in place.
func TestRunStart_NoFromFile_PicksTask(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	existingID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(existingID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, tasks.RequirementsFileName), []byte("# existing\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedTaskRowDirect(t, tasks.Task{
		ID:       existingID,
		Status:   tasks.StatusPlanDone,
		PlanTool: "cursor",
		Summary:  "existing task",
	})
	ui := &scriptedStartUI{
		source:       picker.SourceTask,
		pickedTaskID: existingID,
		pickedTaskOK: true,
	}

	err = RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if ui.taskCalls != 1 {
		t.Fatalf("PickReplanTask calls = %d, want 1", ui.taskCalls)
	}
	rows := allTaskRows(t)
	if len(rows) != 1 {
		t.Fatalf("ListTasks = %d rows, want exactly 1 (re-plan must reuse the existing task)", len(rows))
	}
	if rows[0].ID != existingID {
		t.Fatalf("row id = %q, want %q (no new task should have been minted)", rows[0].ID, existingID)
	}
	row := readTaskFromBolt(t, existingID)
	if row.AgentLogPath == "" {
		t.Fatalf("existing row's AgentLogPath = %q; want non-empty", row.AgentLogPath)
	}
	if row.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q; the orchestrator updates this asynchronously, the parent must leave it as-is", row.Status)
	}
	if row.Summary != "existing task" {
		t.Fatalf("Summary clobbered to %q; want %q", row.Summary, "existing task")
	}
}

// TestRunStart_NoFromFile_PicksLinear drives the source picker into
// the Linear branch with a stubbed GraphQL endpoint and a pre-saved
// API key + project (so the link prompt does not fire). The picker
// supplies the identifier; RunStart fetches the issue, stages
// requirements.md from the markdown body, spawns the orchestrator,
// and seeds a single task row.
func TestRunStart_NoFromFile_PicksLinear(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveProject("p"); err != nil {
		t.Fatal(err)
	}
	srv := testutil.NewLinearStubServer(testutil.LinearStubResponses{
		Issue: &testutil.LinearIssueStub{Identifier: "ENG-7", Title: "picker", Description: "body", URL: "https://linear.app/eng/issue/ENG-7"},
		AssignedIssues: []testutil.LinearIssueStub{
			{Identifier: "ENG-7", Title: "picker", URL: "https://linear.app/eng/issue/ENG-7", State: "Todo"},
		},
	})
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	ui := &scriptedStartUI{
		source:        picker.SourceLinear,
		pickedIssue:   linear.Issue{Identifier: "ENG-7", Title: "picker", State: "Todo"},
		pickedIssueOK: true,
	}

	err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", row.Status)
	}
	tasksDir := tasks.DefaultDir()
	body, err := os.ReadFile(filepath.Join(tasksDir, id, tasks.RequirementsFileName))
	if err != nil {
		t.Fatalf("read requirements.md: %v", err)
	}
	if !strings.HasPrefix(string(body), "# picker") {
		t.Fatalf("requirements.md should start with the issue title; got %q", body)
	}
	if !strings.Contains(string(body), "Linear: https://linear.app/eng/issue/ENG-7") {
		t.Fatalf("requirements.md should carry the Linear URL footer; got %q", body)
	}
}

// TestRunStart_FromLinearFlag pins --from-linear: with linear.api_key
// stored, the flag bypasses the source picker, fetches the issue,
// and seeds a task whose requirements.md carries the rendered body.
func TestRunStart_FromLinearFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	srv := testutil.NewLinearStubServer(testutil.LinearStubResponses{
		Issue: &testutil.LinearIssueStub{Identifier: "ENG-2", Title: "flag", URL: "https://linear.app/eng/issue/ENG-2"},
	})
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	ui := &scriptedStartUI{}
	err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		FromLinear:  "ENG-2",
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if ui.sourceCalls != 0 {
		t.Fatalf("--from-linear should bypass the source picker: sourceCalls=%d", ui.sourceCalls)
	}
	id := firstSeededTaskID(t)
	tasksDir := tasks.DefaultDir()
	body, err := os.ReadFile(filepath.Join(tasksDir, id, tasks.RequirementsFileName))
	if err != nil {
		t.Fatalf("read requirements.md: %v", err)
	}
	if !strings.HasPrefix(string(body), "# flag") {
		t.Fatalf("requirements.md should start with the issue title; got %q", body)
	}
}

// TestRunStart_FromLinearFlag_RecordsLinearIssue pins the
// `linear_issue` round-trip on the task row when --from-linear seeds
// a fresh task: the upstream identifier flows through
// StartTargetFromLinear → StartTarget → persistStartRow into
// task.toml so `j tasks` can keep linking back to Linear.
func TestRunStart_FromLinearFlag_RecordsLinearIssue(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	srv := testutil.NewLinearStubServer(testutil.LinearStubResponses{
		Issue: &testutil.LinearIssueStub{Identifier: "ENG-42", Title: "linked", URL: "https://linear.app/eng/issue/ENG-42"},
	})
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		FromLinear:  "ENG-42",
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.LinearIssue != "ENG-42" {
		t.Fatalf("LinearIssue = %q, want ENG-42", row.LinearIssue)
	}
}

// TestRunStart_FromLinearFlag_MissingKey pins the explicit error
// when --from-linear is supplied but no API key is stored: no task
// is created.
func TestRunStart_FromLinearFlag_MissingKey(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	err := RunStart(t.Context(), StartOptions{
		FromLinear: "ENG-2",
		Stdin:      strings.NewReader(""),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
		Agents:     []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: noopJBinary(t),
	})
	if err == nil || !errors.Is(err, linear.ErrNoAPIKey) {
		t.Fatalf("err = %v, want linear.ErrNoAPIKey", err)
	}
	if rows := allTaskRows(t); len(rows) != 0 {
		t.Fatalf("ListTasks = %d, want 0", len(rows))
	}
}

// (No empty-cwd test here: that branch lives inside
// picker.PickMarkdownInCwd and is exercised by
// internal/cli/picker/picker_test.go::TestPickMarkdownInCwd_NoFiles.)

// TestRunStart_NoFromFile_NoExistingTasks pins the empty-bbolt
// branch on the re-plan source: SourceTask + no rows → wrapped
// error from pickReplanTarget; no spawn.
func TestRunStart_NoFromFile_NoExistingTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	ui := &scriptedStartUI{source: picker.SourceTask}

	err := RunStart(t.Context(), StartOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err == nil || !strings.Contains(err.Error(), "no tasks to re-plan") {
		t.Fatalf("err = %v, want no-tasks-to-replan wrap", err)
	}
}

// TestRunStart_NoFromFile_TaskPickerCancelled pins the picker-abort
// path on the re-plan source: PickReplanTask returns ok=false →
// RunStart exits cleanly with no spawn.
func TestRunStart_NoFromFile_TaskPickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	existingID := tasks.NewTaskID()
	if _, err := tasks.EnsureDir(existingID); err != nil {
		t.Fatal(err)
	}
	seedTaskRowDirect(t, tasks.Task{
		ID:       existingID,
		Status:   tasks.StatusPlanDone,
		PlanTool: "cursor",
		Summary:  "existing",
	})
	ui := &scriptedStartUI{source: picker.SourceTask, pickedTaskOK: false}

	err := RunStart(t.Context(), StartOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      ui,
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (cancelled picker exits cleanly)", err)
	}
	row := readTaskFromBolt(t, existingID)
	if row.AgentLogPath != "" {
		t.Fatalf("existing row's AgentLogPath = %q, want empty (picker cancel must not fire spawn)", row.AgentLogPath)
	}
}

// TestRunStart_ResolveSourceFails pins the read-source error branch:
// pointing --from-file at a non-existent path surfaces a wrapped
// error before any row is seeded.
func TestRunStart_ResolveSourceFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	err := RunStart(t.Context(), StartOptions{
		FromFile: "/definitely/does/not/exist.md",
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: noopJBinary(t),
	})
	if err == nil {
		t.Fatal("expected error from missing --from-file path")
	}
}

// TestRunStart_SpawnFails pins the SpawnIn error branch.
func TestRunStart_SpawnFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	err := RunStart(t.Context(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
}

// TestRunStart_AppliesDefaults exercises StartOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-UI branches) by
// running with populated buckets so the UI is never invoked.
func TestRunStart_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	if err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		FromFile:    target,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},
		JBinary:     noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
}

// TestRunStart_BucketInteractiveUntouched pins one of the plan's
// acceptance criteria: the planner / worker / verifier buckets'
// stored `interactive` flag must be unchanged before vs. after
// `j tasks start`.
func TestRunStart_BucketInteractiveUntouched(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
		path := store.DefaultPath()
		s, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Put(bucket, "interactive", "true"); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
	}
	target := writeStartFile(t, "# task\nbody")
	if err := RunStart(t.Context(), StartOptions{
		Interactive: new(false),
		FromFile:    target,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		_, _, interactive := testutil.ReadAgentBucket(t, bucket)
		if interactive != "true" {
			t.Fatalf("bucket %q interactive = %q, want unchanged \"true\"", bucket, interactive)
		}
	}
}

// TestNewStartCmd_FlagDefaults pins the registered flag set.
func TestNewStartCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	if cmd.Use != "start" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	// pflag visits flags in lexicographic order.
	want := []string{"from-file", "from-linear", "from-task", "interactive", "model", "plan-requires-approval", "tool", "yes"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", names, want)
	}
}

// TestNewStartCmd_FlagsBindToViper covers flag→viper bindings.
func TestNewStartCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	for flag, key := range map[string]string{
		"from-file":   "tasks.start.from_file",
		"from-linear": "tasks.start.from_linear",
		"from-task":   "tasks.start.from_task",
		"tool":        "tasks.start.tool",
		"model":       "tasks.start.model",
	} {
		if err := cmd.Flags().Set(flag, "testval"); err != nil {
			t.Fatalf("Flags().Set %s: %v", flag, err)
		}
		if got := viper.GetString(key); got != "testval" {
			t.Errorf("%s = %q, want testval", key, got)
		}
	}
	for flag, key := range map[string]string{
		"plan-requires-approval": "tasks.start.plan_requires_approval",
		"interactive":            "tasks.start.interactive",
		"yes":                    "tasks.start.yes",
	} {
		if err := cmd.Flags().Set(flag, "true"); err != nil {
			t.Fatalf("Flags().Set %s: %v", flag, err)
		}
		if got := viper.GetBool(key); !got {
			t.Errorf("%s = false, want true", key)
		}
	}
}

// TestNewStartCmd_EnvBindings covers env-var→viper bindings.
func TestNewStartCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_START_FROM_FILE", "/env/foo.md")
	t.Setenv("TASKS_START_FROM_LINEAR", "ENG-1")
	t.Setenv("TASKS_START_FROM_TASK", "task-id")
	t.Setenv("TASKS_START_TOOL", "claude")
	t.Setenv("TASKS_START_MODEL", "opus")
	t.Setenv("TASKS_START_INTERACTIVE", "true")
	t.Setenv("TASKS_START_YES", "true")
	t.Setenv("TASKS_START_PLAN_REQUIRES_APPROVAL", "true")
	_ = newStartCmd()
	if got := viper.GetString("tasks.start.from_file"); got != "/env/foo.md" {
		t.Errorf("tasks.start.from_file = %q", got)
	}
	if got := viper.GetString("tasks.start.from_linear"); got != "ENG-1" {
		t.Errorf("tasks.start.from_linear = %q", got)
	}
	if got := viper.GetString("tasks.start.from_task"); got != "task-id" {
		t.Errorf("tasks.start.from_task = %q", got)
	}
	if got := viper.GetString("tasks.start.tool"); got != "claude" {
		t.Errorf("tasks.start.tool = %q", got)
	}
	if got := viper.GetString("tasks.start.model"); got != "opus" {
		t.Errorf("tasks.start.model = %q", got)
	}
	if got := viper.GetBool("tasks.start.interactive"); !got {
		t.Errorf("tasks.start.interactive = false, want true")
	}
	if got := viper.GetBool("tasks.start.yes"); !got {
		t.Errorf("tasks.start.yes = false, want true")
	}
	if got := viper.GetBool("tasks.start.plan_requires_approval"); !got {
		t.Errorf("tasks.start.plan_requires_approval = false, want true")
	}
}

func TestStartPlanRequiresApprovalOverride_NoFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	got, err := startPlanRequiresApprovalOverride(cmd)
	if err != nil {
		t.Fatalf("startPlanRequiresApprovalOverride: %v", err)
	}
	if got != nil {
		t.Fatalf("override = %v, want nil", *got)
	}
}

func TestStartPlanRequiresApprovalOverride_ExplicitFalse(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	if err := cmd.Flags().Set("plan-requires-approval", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	got, err := startPlanRequiresApprovalOverride(cmd)
	if err != nil {
		t.Fatalf("startPlanRequiresApprovalOverride: %v", err)
	}
	if got == nil || *got {
		t.Fatalf("override = %v, want false", got)
	}
}

// TestNewStartCmd_RunE_PropagatesError exercises the RunE closure
// end to end with a nonexistent --from-file path.
func TestNewStartCmd_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	cmd := newStartCmd()
	if err := cmd.Flags().Set("from-file", "/does/not/exist.md"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected an error from missing --from-file path")
	}
}

// TestRunStart_RegisteredAsChild verifies `j tasks start` is wired
// as a cobra child of `j tasks`.
func TestRunStart_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "start" {
			return
		}
	}
	t.Fatal("`j tasks start` should be registered as a child of `j tasks`")
}

// TestResolveJBinary_Default exercises the os.Executable fallback.
func TestResolveJBinary_Default(t *testing.T) {
	got, err := resolveJBinary("")
	if err != nil {
		t.Fatalf("resolveJBinary: %v", err)
	}
	if got == "" {
		t.Fatalf("resolveJBinary(\"\") returned empty path")
	}
}

// TestResolveJBinary_Override exercises the explicit-override branch.
func TestResolveJBinary_Override(t *testing.T) {
	got, err := resolveJBinary("/explicit/j")
	if err != nil {
		t.Fatalf("resolveJBinary: %v", err)
	}
	if got != "/explicit/j" {
		t.Fatalf("resolveJBinary(/explicit/j) = %q", got)
	}
}

// TestRunStart_ContextCancellable pins ctx-cancellation propagation.
func TestRunStart_ContextCancellable(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := RunStart(ctx, StartOptions{
		Interactive: new(false),
		FromFile:    target,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},

		UI:      &scriptedStartUI{},
		JBinary: noopJBinary(t),
	})
	if err == nil {
		_ = firstSeededTaskID(t)
		return
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context-cancellation propagation", err)
	}
}

// TestRunStart_ArgvParsesThroughOrchestrateCmd is the regression
// guard for the pflag two-token bool bug on the start spawn: the
// argv must parse through a fresh `j tasks orchestrate` cobra
// command with the requested plan-requires-approval bool. Catches
// any future revert to the `"--flag", "value"` shape because pflag
// would mark the bool flag Changed=true regardless of the next
// token, leaving the bool at its default (true) and dropping the
// override.
func TestRunStart_ArgvParsesThroughOrchestrateCmd(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }
	tests := []struct {
		name     string
		setting  string
		override *bool
		want     bool
	}{
		{name: "inherits_true_setting", setting: "true", want: true},
		{name: "explicit_false_overrides_true", setting: "true", override: boolPtr(false), want: false},
		{name: "explicit_true_overrides_false", setting: "false", override: boolPtr(true), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			putProjectPlanRequiresApproval(t, tc.setting)
			for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
				testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
			}
			target := writeStartFile(t, "# task\nbody")
			argvPath := filepath.Join(t.TempDir(), "argv.txt")
			if err := RunStart(t.Context(), StartOptions{
				Interactive:          boolPtr(false),
				FromFile:             target,
				PlanRequiresApproval: tc.override,
				Stdin:                strings.NewReader(""),
				Stdout:               io.Discard,
				Stderr:               io.Discard,
				Agents:               []codingagents.Agent{testutil.NewScriptedAgent()},

				UI:      &scriptedStartUI{},
				JBinary: argvJBinary(t, argvPath),
			}); err != nil {
				t.Fatalf("RunStart: %v", err)
			}
			args := readSpawnedArgv(t, argvPath)
			if len(args) < 2 || args[0] != "tasks" || args[1] != "orchestrate" {
				t.Fatalf("argv = %v, want leading `tasks orchestrate`", args)
			}
			viper.Reset()
			t.Cleanup(viper.Reset)
			cmd := newOrchestrateCmd()
			if err := cmd.ParseFlags(args[2:]); err != nil {
				t.Fatalf("ParseFlags(%v): %v", args[2:], err)
			}
			if !cmd.Flags().Changed("plan-requires-approval") {
				t.Fatalf("plan-requires-approval not Changed; argv=%v", args)
			}
			got, err := cmd.Flags().GetBool("plan-requires-approval")
			if err != nil {
				t.Fatalf("GetBool: %v", err)
			}
			if got != tc.want {
				t.Fatalf("plan-requires-approval = %v, want %v; argv=%v", got, tc.want, args)
			}
		})
	}
}

// TestRunStart_InteractiveRunsInline pins the foreground path:
// --interactive=true re-execs `j tasks orchestrate` inline (blocking,
// terminal-attached). The fork dialog must not fire — the orchestrator
// runs inline and the parent never spawns a detached child.
func TestRunStart_InteractiveRunsInline(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	yes := true
	var stdout bytes.Buffer
	if err := RunStart(t.Context(), StartOptions{
		FromFile:    target,
		Interactive: &yes,
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:          &scriptedStartUI{},
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" || args[1] != "orchestrate" {
		t.Fatalf("argv = %v, want leading `tasks orchestrate`", args)
	}
	if !containsArg(args, "--interactive=true") {
		t.Fatalf("argv = %v, want --interactive=true", args)
	}
	if strings.Contains(stdout.String(), "running in background") || strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout = %q, want no fork dialog (inline exec)", stdout.String())
	}
	_ = firstSeededTaskID(t)
}

// containsArg reports whether want appears in args. Tiny helper for
// the inline-exec test, which only cares that --interactive=true is
// present somewhere in the forwarded argv (its position relative to
// the other one-off overrides is not load-bearing).
func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

// seedTaskRowDirect inserts a Task row via the per-project tasks
// bbolt DB. Used by the re-plan tests to pre-seed an existing task
// without going through any phase lifecycle.
func seedTaskRowDirect(t *testing.T, row tasks.Task) {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}
