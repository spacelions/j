package codex

import (
	"reflect"
	"strings"
	"testing"
)

func TestAgent_Name(t *testing.T) {
	if got := New().Name(); got != "codex" {
		t.Fatalf("Name = %q, want %q", got, "codex")
	}
}

// TestNewResumeID_AlwaysEmpty pins the contract: codex has no pre-run
// session-id binding flag, so NewResumeID always returns ("", nil)
// regardless of how many times it is called.
func TestNewResumeID_AlwaysEmpty(t *testing.T) {
	a := New()
	for range 3 {
		got, err := a.NewResumeID(t.Context())
		if err != nil {
			t.Fatalf("NewResumeID: %v", err)
		}
		if got != "" {
			t.Fatalf("NewResumeID = %q, want empty", got)
		}
	}
}

// TestListModels_StaticAliases pins the static picker list and
// asserts ListModels returns a fresh copy (callers must not be able
// to mutate the package state).
func TestListModels_StaticAliases(t *testing.T) {
	a := New()
	got, err := a.ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"gpt-5.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListModels = %v, want %v", got, want)
	}
	got[0] = "MUTATED"
	again, err := New().ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if again[0] == "MUTATED" {
		t.Fatalf(
			"ListModels returned a shared slice — caller mutation leaked: %v",
			again,
		)
	}
}

// TestInteractiveArgs pins the argv built for the interactive
// Work / Verify entrypoint: fresh runs go straight to
// `codex [-m m] -- <prompt>`, resume runs prepend `resume <id>`, and
// the literal `--` separator always lands so a leading-dash prompt
// body is not parsed as a flag.
func TestInteractiveArgs(t *testing.T) {
	cases := []struct {
		name, resume, model, prompt string
		want                        []string
	}{
		{
			"fresh-with-model", "", "gpt-5.5", "do work",
			[]string{"-m", "gpt-5.5", "--", "do work"},
		},
		{
			"fresh-no-model", "", "", "do work",
			[]string{"--", "do work"},
		},
		{
			"resume-with-model", "abc", "gpt-5.5", "do work",
			[]string{"resume", "abc", "-m", "gpt-5.5", "--", "do work"},
		},
		{
			"resume-no-model", "abc", "", "do work",
			[]string{"resume", "abc", "--", "do work"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := interactiveArgs(tc.resume, tc.model, tc.prompt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("interactiveArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInteractivePlannerArgs pins the argv built for interactive
// planning. It adds read-only sandboxing and approval-on-request
// behavior while preserving resume, model, and prompt placement.
func TestInteractivePlannerArgs(t *testing.T) {
	cases := []struct {
		name, resume, model, prompt string
		want                        []string
	}{
		{
			"fresh-with-model", "", "gpt-5.5", "do work",
			[]string{
				"-m", "gpt-5.5",
				"--ask-for-approval", "on-request",
				"--sandbox", "read-only",
				"--", "do work",
			},
		},
		{
			"fresh-no-model", "", "", "do work",
			[]string{
				"--ask-for-approval", "on-request",
				"--sandbox", "read-only",
				"--", "do work",
			},
		},
		{
			"resume-with-model", "abc", "gpt-5.5", "do work",
			[]string{
				"resume", "abc", "-m", "gpt-5.5",
				"--ask-for-approval", "on-request",
				"--sandbox", "read-only",
				"--", "do work",
			},
		},
		{
			"resume-no-model", "abc", "", "do work",
			[]string{
				"resume", "abc",
				"--ask-for-approval", "on-request",
				"--sandbox", "read-only",
				"--", "do work",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := interactivePlannerArgs(
				tc.resume, tc.model, tc.prompt,
			)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf(
					"interactivePlannerArgs = %v, want %v",
					got, tc.want,
				)
			}
		})
	}
}

// TestHeadlessArgs pins the argv built for the `exec` entrypoint:
// the bypass + skip-git-repo-check + JSON flags always land, the
// prompt sits behind a literal `--`, and resume runs splice
// `resume <id>` after `exec`.
func TestHeadlessArgs(t *testing.T) {
	cases := []struct {
		name, resume, model, prompt string
		want                        []string
	}{
		{
			"fresh", "", "gpt-5.5", "do work",
			[]string{
				"exec", "-m", "gpt-5.5",
				"--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"--json",
				"--", "do work",
			},
		},
		{
			"fresh-no-model", "", "", "do work",
			[]string{
				"exec",
				"--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"--json",
				"--", "do work",
			},
		},
		{
			"resume", "abc", "gpt-5.5", "do work",
			[]string{
				"exec", "resume", "abc", "-m", "gpt-5.5",
				"--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"--json",
				"--", "do work",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := headlessArgs(tc.resume, tc.model, tc.prompt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("headlessArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAppendModel pins the empty-vs-nonempty branches of the model
// argv helper: empty model leaves args untouched.
func TestAppendModel(t *testing.T) {
	if got := appendModel(nil, ""); got != nil {
		t.Fatalf("appendModel(nil, \"\") = %v, want nil", got)
	}
	got := appendModel([]string{"exec"}, "gpt-5.5")
	want := []string{"exec", "-m", "gpt-5.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendModel = %v, want %v", got, want)
	}
}

func TestFormatLog_Passthrough(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		[]byte("\n"),
		[]byte("plain log line\n"),
		[]byte(`{"type":"future.event","value":1}` + "\n"),
		[]byte(`{"type":"item.completed"}` + "\n"),
		[]byte(
			`{"type":"item.completed","item":{"type":"future"}}` +
				"\n",
		),
		[]byte(
			`{"type":"item.completed","item":{` +
				`"type":"file_change","changes":"bad"}}` + "\n",
		),
		[]byte("\xff\xfe binary bytes \x00 mid line"),
	}
	for _, in := range cases {
		got := New().FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want passthrough", in, got)
		}
	}
}

func TestFormatLog_TopLevelEvents(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []string
	}{
		{
			name: "thread",
			in:   []byte(`{"type":"thread.started","thread_id":"t1"}`),
			want: []string{"agent thread", "thread_id=t1"},
		},
		{
			name: "turn-started",
			in:   []byte(`{"type":"turn.started"}`),
			want: []string{"agent status", "status=turn_started"},
		},
		{
			name: "turn-completed",
			in: []byte(`{"type":"turn.completed","usage":{` +
				`"input_tokens":1,"cached_input_tokens":2,` +
				`"output_tokens":3,"reasoning_output_tokens":4}}`),
			want: []string{
				"agent result",
				"input_tokens=1",
				"cached_input_tokens=2",
				"output_tokens=3",
				"reasoning_output_tokens=4",
			},
		},
		{
			name: "turn-failed",
			in: []byte(`{"type":"turn.failed",` +
				`"error":{"message":"nope"}}`),
			want: []string{"agent error", "message=nope"},
		},
		{
			name: "error",
			in:   []byte(`{"type":"error","message":"broken"}`),
			want: []string{"agent error", "message=broken"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertContainsAll(t, string(New().FormatLog(tc.in)), tc.want)
		})
	}
}

func TestFormatLog_ItemEvents(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []string
	}{
		{
			name: "message",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"agent_message","text":"done"}}`),
			want: []string{"agent message", "text=done"},
		},
		{
			name: "reasoning",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"reasoning","text":"thinking"}}`),
			want: []string{"agent thinking", "text=thinking"},
		},
		{
			name: "command-started",
			in: []byte(`{"type":"item.started","item":{` +
				`"id":"i1","type":"command_execution",` +
				`"command":"go test ./...","status":"in_progress"}}`),
			want: []string{
				"agent command",
				"command=go test ./...",
				"phase=started",
				"status=in_progress",
			},
		},
		{
			name: "file-change",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"file_change",` +
				`"status":"completed","changes":[` +
				`{"path":"a.go","kind":"update"},` +
				`{"path":"b.go","kind":"add"}]}}`),
			want: []string{
				"agent file_change",
				"changes=2",
				"files=update:a.go,add:b.go",
				"status=completed",
			},
		},
		{
			name: "file-change-many",
			in: []byte(`{"type":"item.completed","item":{` +
				`"type":"file_change","status":"completed",` +
				`"changes":[` +
				`{"path":"a.go","kind":"update"},` +
				`{"path":"b.go","kind":"update"},` +
				`{"path":"c.go","kind":"update"},` +
				`{"path":"d.go","kind":"update"},` +
				`{"path":"e.go","kind":"update"},` +
				`{"path":"f.go","kind":"update"}]}}`),
			want: []string{
				"agent file_change",
				"changes=6",
				"files=update:a.go,update:b.go,update:c.go,",
				"+1",
			},
		},
		{
			name: "mcp",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"mcp_tool_call","server":"s",` +
				`"tool":"fetch","status":"failed",` +
				`"error":{"message":"bad"}}}`),
			want: []string{
				"agent mcp_tool_call",
				"server=s",
				"tool=fetch",
				"status=failed",
				"message=bad",
			},
		},
		{
			name: "web-search",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"web_search","query":"golang",` +
				`"action":{"type":"search"}}}`),
			want: []string{
				"agent web_search",
				"query=golang",
				"action=search",
			},
		},
		{
			name: "web-search-string-action",
			in: []byte(`{"type":"item.completed","item":{` +
				`"type":"web_search","query":"golang",` +
				`"action":"open_page"}}`),
			want: []string{"agent web_search", "action=open_page"},
		},
		{
			name: "web-search-empty-action",
			in: []byte(`{"type":"item.completed","item":{` +
				`"type":"web_search","query":"golang"}}`),
			want: []string{"agent web_search", "query=golang"},
		},
		{
			name: "web-search-invalid-action",
			in: []byte(`{"type":"item.completed","item":{` +
				`"type":"web_search","query":"golang","action":7}}`),
			want: []string{"agent web_search", "query=golang"},
		},
		{
			name: "todo",
			in: []byte(`{"type":"item.updated","item":{` +
				`"id":"i1","type":"todo_list","items":[` +
				`{"text":"done","completed":true},` +
				`{"text":"next","completed":false}]}}`),
			want: []string{
				"agent todo_list",
				"items=2",
				"completed=1",
				"pending=1",
				"current=next",
			},
		},
		{
			name: "error",
			in: []byte(`{"type":"item.completed","item":{` +
				`"id":"i1","type":"error","message":"warn"}}`),
			want: []string{"agent error", "message=warn"},
		},
		{
			name: "error-object",
			in: []byte(`{"type":"item.completed","item":{` +
				`"type":"error","error":{"message":"nested"}}}`),
			want: []string{"agent error", "message=nested"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertContainsAll(t, string(New().FormatLog(tc.in)), tc.want)
		})
	}
}

func TestFormatLog_CommandOutputOmitted(t *testing.T) {
	src := []byte(`{"type":"item.completed","item":{` +
		`"id":"i1","type":"command_execution","command":"go test",` +
		`"aggregated_output":"very noisy command output",` +
		`"exit_code":0,"status":"completed"}}`)

	got := string(New().FormatLog(src))
	assertContainsAll(t, got, []string{
		"agent command",
		"command=go test",
		"exit_code=0",
		"output_bytes=25",
		"status=completed",
	})
	if strings.Contains(got, "very noisy command output") {
		t.Fatalf("aggregated output leaked: %q", got)
	}
}

func TestFormatLog_TextTruncation(t *testing.T) {
	long := strings.Repeat("x", 250)
	src := []byte(`{"type":"item.completed","item":{` +
		`"id":"i1","type":"agent_message","text":"` + long + `"}}`)

	got := string(New().FormatLog(src))
	assertContainsAll(t, got, []string{"agent message", "chars=250"})
	if strings.Contains(got, long) {
		t.Fatalf("untruncated body leaked: %q", got)
	}
}

func TestFormatLog_RenamedTextTruncation(t *testing.T) {
	long := strings.Repeat("q", 250)
	src := []byte(`{"type":"item.completed","item":{` +
		`"type":"web_search","query":"` + long + `"}}`)

	got := string(New().FormatLog(src))
	assertContainsAll(t, got, []string{
		"agent web_search",
		"query_chars=250",
	})
	if strings.Contains(got, " chars=250") {
		t.Fatalf("unrenamed chars field leaked: %q", got)
	}
}

func assertContainsAll(t *testing.T, got string, want []string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in %q", s, got)
		}
	}
}
