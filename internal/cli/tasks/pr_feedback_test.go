package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
	"github.com/spacelions/j/internal/tools/linear"
)

func TestRunPRFeedback_AcceptedFileTaskWritesArtifact(t *testing.T) {
	env := newPRFeedbackEnv(t, func(task *storetasks.Task) {
		task.Status = storetasks.StatusCompleted
	})
	agent := &prFeedbackAgent{body: "Summary\n\nDecision: changes needed\n"}
	out, err := runPRFeedbackWithAgent(t, env.invocation(), agent)
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(out, "accepted: task "+env.task.ID) {
		t.Fatalf("stdout = %q, want accepted task", out)
	}
	body := readTaskFile(t, env.task.ID, prFeedbackPlanFileName)
	if !strings.Contains(body, "Decision: changes needed") {
		t.Fatalf("artifact = %q", body)
	}
	if readTaskFile(t, env.task.ID, storetasks.PlanFileName) != "normal plan" {
		t.Fatal("normal plan.md was overwritten")
	}
	if agent.workCalls != 0 || agent.verifyCalls != 0 {
		t.Fatalf("work=%d verify=%d, want planner only",
			agent.workCalls, agent.verifyCalls)
	}
	if agent.lastReq.PRFeedback == nil {
		t.Fatal("PlanRequest.PRFeedback is nil")
	}
	got := testutil.ReadTaskRow(t, env.task.ID)
	if !hasProcessedCommand(got, env.invocation().CommentID) {
		t.Fatal("command id was not persisted")
	}
}

func TestRunPRFeedback_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*PRFeedbackInvocation, *storetasks.Task)
		seedDup bool
		second  bool
		want    string
	}{
		{
			name: "invalid command",
			mutate: func(inv *PRFeedbackInvocation, _ *storetasks.Task) {
				inv.CommentBody = "please review"
			},
			want: "ignored: invalid command",
		},
		{
			name: "missing command id",
			mutate: func(inv *PRFeedbackInvocation, _ *storetasks.Task) {
				inv.CommentID = " "
			},
			want: "rejected: invalid command id",
		},
		{
			name: "bot author",
			mutate: func(inv *PRFeedbackInvocation, _ *storetasks.Task) {
				inv.CommentAuthorBot = true
			},
			want: "rejected: bot users are not allowed",
		},
		{
			name: "unauthorized author",
			mutate: func(inv *PRFeedbackInvocation, _ *storetasks.Task) {
				inv.CommentAuthor = "reviewer"
			},
			want: "rejected: unauthorized author",
		},
		{
			name: "no matching task",
			mutate: func(inv *PRFeedbackInvocation, _ *storetasks.Task) {
				inv.PullRequestURL = "https://github.com/o/r/pull/2"
			},
			want: "rejected: no matching task",
		},
		{
			name:    "ambiguous task",
			second:  true,
			want:    "rejected: ambiguous task",
			mutate:  func(*PRFeedbackInvocation, *storetasks.Task) {},
			seedDup: false,
		},
		{
			name:    "duplicate command",
			seedDup: true,
			want:    "rejected: duplicate command",
			mutate:  func(*PRFeedbackInvocation, *storetasks.Task) {},
		},
		{
			name: "running task",
			mutate: func(_ *PRFeedbackInvocation, task *storetasks.Task) {
				task.Status = storetasks.StatusWorking
			},
			want: "rejected: locked/running task",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := newPRFeedbackEnv(t, func(task *storetasks.Task) {
				if tt.seedDup {
					task.ProcessedPRCommands = []string{"c1"}
				}
			})
			inv := env.invocation()
			tt.mutate(&inv, &env.task)
			testutil.SeedTaskRow(t, env.task)
			if tt.second {
				seedPRFeedbackTask(t, func(task *storetasks.Task) {
					task.PullRequestURL = env.task.PullRequestURL
				})
			}
			out, err := runPRFeedbackWithAgent(t, inv, &prFeedbackAgent{})
			if err != nil {
				t.Fatalf("RunPRFeedback: %v", err)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("stdout = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestRunPRFeedback_LockedTaskRejected(t *testing.T) {
	env := newPRFeedbackEnv(t, nil)
	lock, err := storetasks.AcquireLock(t.Context(), env.task.ID)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() { _ = lock.Release() }()
	out, err := runPRFeedbackWithAgent(t, env.invocation(), &prFeedbackAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(out, "rejected: locked/running task") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRunPRFeedback_PlannerErrors(t *testing.T) {
	env := newPRFeedbackEnv(t, nil)
	inv := env.invocation()
	if _, err := runPRFeedbackWithAgent(t, inv, &prFeedbackAgent{
		planErr: errors.New("plan boom"),
	}); err == nil || !strings.Contains(err.Error(), "plan boom") {
		t.Fatalf("plan error = %v", err)
	}
	if _, err := runPRFeedbackWithAgent(t, inv, &prFeedbackAgent{}); err == nil ||
		!strings.Contains(err.Error(), "read artifact") {
		t.Fatalf("missing artifact error = %v", err)
	}
	if _, err := runPRFeedbackWithAgent(t, inv, nil); err == nil ||
		!strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("resolver error = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := runPRFeedbackWithContext(
		cancelled, t, inv, &prFeedbackAgent{body: "x", pid: os.Getpid()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context canceled", err)
	}
	s, err := storetasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	defer func() { _ = s.Close() }()
	task := env.task
	task.Status = "not-valid"
	if _, err := runPRFeedbackPlanner(
		t.Context(),
		PRFeedbackOptions{
			Agents: []codingagents.Agent{&prFeedbackAgent{body: "x"}},
			Tool:   "scripted",
			Model:  "m1",
			Stderr: io.Discard,
		},
		inv,
		s,
		task,
	); err == nil || !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("PutTask error = %v", err)
	}
}

func TestRunPRFeedback_StoreErrors(t *testing.T) {
	env := newPRFeedbackEnv(t, nil)
	if err := RunPRFeedback(t.Context(), PRFeedbackOptions{
		InputPath: "missing.json",
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	}); err == nil || !strings.Contains(err.Error(), "read input") {
		t.Fatalf("input error = %v", err)
	}
	testutil.SeedRawTaskFile(t, "bad", []byte("not = valid = toml"))
	if _, err := runPRFeedbackWithAgent(
		t, env.invocation(), &prFeedbackAgent{body: "x"},
	); err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("list error = %v", err)
	}
	if err := runLockedPRFeedback(
		t.Context(),
		PRFeedbackOptions{Stdout: io.Discard, Stderr: io.Discard},
		env.invocation(),
		storetasks.Open(t.TempDir()),
		storetasks.Task{},
	); err == nil || !strings.Contains(err.Error(), "empty task id") {
		t.Fatalf("lock error = %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "child")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	t.Chdir(dir)
	t.Setenv("PWD", "")
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatalf("Chmod parent: %v", err)
	}
	defer func() { _ = os.Chmod(parent, 0o755) }()
	_, err = runPRFeedbackWithAgent(t, env.invocation(), &prFeedbackAgent{})
	if chmodErr := os.Chmod(parent, 0o755); chmodErr != nil {
		t.Fatalf("restore chmod: %v", chmodErr)
	}
	t.Chdir(prev)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("tasks dir error = %v", err)
	}
}

func TestPRFeedbackLoadInvocation(t *testing.T) {
	direct := PRFeedbackOptions{Invocation: PRFeedbackInvocation{CommentID: "d"}}
	if got, err := direct.loadInvocation(); err != nil || got.CommentID != "d" {
		t.Fatalf("direct load = %#v, %v", got, err)
	}
	if _, err := (PRFeedbackOptions{InputPath: "missing.json"}).
		loadInvocation(); err == nil || !strings.Contains(err.Error(), "read input") {
		t.Fatalf("read error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	if _, err := (PRFeedbackOptions{InputPath: path}).
		loadInvocation(); err == nil ||
		!strings.Contains(err.Error(), "decode input") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestPRFeedbackCommandRunsFromInput(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path := filepath.Join(t.TempDir(), "payload.json")
	inv := PRFeedbackInvocation{CommentBody: "hello"}
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, newPRFeedbackCmd(), "--input", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "ignored: invalid command") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestPRFeedbackMatchingHelpers(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	task := seedPRFeedbackTask(t, nil)
	s, err := storetasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	defer func() { _ = s.Close() }()
	matches, err := tasksByPRURL(s,
		"HTTPS://github.com/O/R/pull/1/?utm=x#discussion!")
	if err != nil || len(matches) != 1 || matches[0].ID != task.ID {
		t.Fatalf("matches = %#v, %v", matches, err)
	}
	if got := normalizePRURL("://bad"); got != "://bad" {
		t.Fatalf("normalize bad URL = %q", got)
	}
	if !isTakeALookCommand(" \n(@J take   a LOOK!!!") {
		t.Fatal("command parser rejected allowlisted command")
	}
	if isTakeALookCommand("please @j take a look") {
		t.Fatal("command parser accepted surrounding words")
	}
	if !isBotUser("dependabot[bot]", false) || !isBotUser("u", true) {
		t.Fatal("bot detector missed a bot")
	}
	if !sameLogin(" Alice ", "alice") {
		t.Fatal("sameLogin should trim and fold case")
	}
	testutil.SeedRawTaskFile(t, "bad", []byte("not = valid = toml"))
	if _, err := tasksByPRURL(s, task.PullRequestURL); err == nil {
		t.Fatal("expected corrupt task decode error")
	}
}

func TestPostPRFeedbackToLinear(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	postPRFeedbackToLinear(t.Context(), &stderr, storetasks.Task{}, "body")
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	postPRFeedbackToLinear(t.Context(), &stderr,
		storetasks.Task{LinearIssue: "SPA-1"}, "body")
	if !strings.Contains(stderr.String(), "no API key set") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	stderr.Reset()
	breakLinearSettings(t)
	postPRFeedbackToLinear(t.Context(), &stderr,
		storetasks.Task{LinearIssue: "SPA-1"}, "body")
	if !strings.Contains(stderr.String(), "load api key") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPostPRFeedbackToLinearHTTP(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := linear.SaveAPIKey("lin_api_TEST"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	cases := []struct {
		name      string
		commentOK bool
		wantErr   string
	}{
		{name: "success", commentOK: true},
		{name: "comment error", wantErr: "commentCreate"},
		{name: "resolve error", wantErr: "resolve"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			srv := prFeedbackLinearServer(t, tt.name, tt.commentOK)
			defer srv.Close()
			prev := linear.TestEndpoint
			linear.TestEndpoint = srv.URL
			t.Cleanup(func() { linear.TestEndpoint = prev })
			postPRFeedbackToLinear(t.Context(), &stderr,
				storetasks.Task{LinearIssue: "SPA-1"}, "artifact body")
			if tt.wantErr == "" && stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if tt.wantErr != "" &&
				!strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want %q",
					stderr.String(), tt.wantErr)
			}
		})
	}
}

type prFeedbackEnv struct {
	task storetasks.Task
}

func newPRFeedbackEnv(
	t *testing.T,
	mutate func(*storetasks.Task),
) prFeedbackEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	task := seedPRFeedbackTask(t, mutate)
	return prFeedbackEnv{task: task}
}

func (e prFeedbackEnv) invocation() PRFeedbackInvocation {
	return PRFeedbackInvocation{
		PullRequestURL:    e.task.PullRequestURL,
		PullRequestTitle:  "Add feature",
		PullRequestAuthor: "alice",
		CommentID:         "c1",
		CommentAuthor:     "ALICE",
		CommentBody:       " @J take a look! ",
		Comments: []PRFeedbackComment{{
			ID: "r1", Author: "reviewer", Body: "Please add a test.",
			URL: "https://github.com/o/r/pull/1#discussion_r1",
		}},
	}
}

func seedPRFeedbackTask(
	t *testing.T,
	mutate func(*storetasks.Task),
) storetasks.Task {
	t.Helper()
	id := storetasks.NewTaskID()
	dir, err := storetasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(dir, storetasks.RequirementsFileName), "req",
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(dir, storetasks.PlanFileName), "normal plan",
	); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	task := storetasks.Task{
		ID:             id,
		Status:         storetasks.StatusWorkDone,
		Summary:        "seed",
		PullRequestURL: "https://github.com/o/r/pull/1",
	}
	if mutate != nil {
		mutate(&task)
	}
	testutil.SeedTaskRow(t, task)
	return task
}

func runPRFeedbackWithAgent(
	t *testing.T,
	inv PRFeedbackInvocation,
	agent codingagents.Agent,
) (string, error) {
	t.Helper()
	return runPRFeedbackWithContext(t.Context(), t, inv, agent)
}

func runPRFeedbackWithContext(
	ctx context.Context,
	t *testing.T,
	inv PRFeedbackInvocation,
	agent codingagents.Agent,
) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	agents := []codingagents.Agent{}
	if agent != nil {
		agents = append(agents, agent)
	}
	err := RunPRFeedback(ctx, PRFeedbackOptions{
		Invocation: inv,
		Tool:       "scripted",
		Model:      "m1",
		Stdout:     &stdout,
		Stderr:     io.Discard,
		Agents:     agents,
	})
	return stdout.String(), err
}

func readTaskFile(t *testing.T, id, name string) string {
	t.Helper()
	dir, err := storetasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, id, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func breakLinearSettings(t *testing.T) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove settings: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
}

func prFeedbackLinearServer(
	t *testing.T,
	mode string,
	commentOK bool,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		raw, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		body := string(raw)
		switch {
		case mode == "resolve error" && strings.Contains(body, "issue(id:"):
			_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
		case strings.Contains(body, "issue(id:"):
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"issue-node"}}}`)
		case strings.Contains(body, "commentCreate") && commentOK:
			_, _ = io.WriteString(w,
				`{"data":{"commentCreate":{"success":true}}}`)
		case strings.Contains(body, "commentCreate"):
			http.Error(w, "bad comment", http.StatusBadRequest)
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
}

type prFeedbackAgent struct {
	body        string
	planErr     error
	pid         int
	lastReq     codingagents.PlanRequest
	workCalls   int
	verifyCalls int
}

func (a *prFeedbackAgent) Name() string { return "scripted" }

func (a *prFeedbackAgent) ListModels(context.Context) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *prFeedbackAgent) CheckLogin(context.Context) error { return nil }

func (a *prFeedbackAgent) NewResumeID(context.Context) (string, error) {
	return "", nil
}

func (a *prFeedbackAgent) Plan(
	_ context.Context,
	req codingagents.PlanRequest,
) (int, error) {
	a.lastReq = req
	if a.planErr != nil {
		return 0, a.planErr
	}
	if a.body != "" {
		if err := os.WriteFile(
			req.PlanOutputPath, []byte(a.body), 0o644,
		); err != nil {
			return 0, err
		}
	}
	return a.pid, nil
}

func (a *prFeedbackAgent) Work(
	context.Context,
	codingagents.WorkRequest,
) (int, error) {
	a.workCalls++
	return 0, nil
}

func (a *prFeedbackAgent) Verify(
	context.Context,
	codingagents.VerifyRequest,
) (int, error) {
	a.verifyCalls++
	return 0, nil
}

func (*prFeedbackAgent) FormatLog(line []byte) []byte { return line }

func TestPRFeedbackDefaults(t *testing.T) {
	opts := PRFeedbackOptions{}.withDefaults()
	if opts.Stdout == nil || opts.Stderr == nil {
		t.Fatal("defaults did not fill writers")
	}
}
