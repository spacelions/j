package codex

import (
	"reflect"
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
// entrypoint: fresh runs go straight to `codex [-m m] -- <prompt>`,
// resume runs prepend `resume <id>`, and the literal `--` separator
// always lands so a leading-dash prompt body is not parsed as a flag.
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

// TestHeadlessArgs pins the argv built for the `exec` entrypoint:
// the bypass + skip-git-repo-check flags always land, the prompt sits
// behind a literal `--`, and resume runs splice `resume <id>` after
// `exec`.
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
				"--", "do work",
			},
		},
		{
			"fresh-no-model", "", "", "do work",
			[]string{
				"exec",
				"--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
				"--", "do work",
			},
		},
		{
			"resume", "abc", "gpt-5.5", "do work",
			[]string{
				"exec", "resume", "abc", "-m", "gpt-5.5",
				"--skip-git-repo-check",
				"--dangerously-bypass-approvals-and-sandbox",
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

// TestFormatLog_Identity pins the formatter contract: every input
// line passes through unchanged. codex's `exec` entrypoint emits
// human text rather than stream-json so there is nothing to render.
func TestFormatLog_Identity(t *testing.T) {
	a := New()
	cases := [][]byte{
		nil,
		{},
		[]byte("\n"),
		[]byte("plain log line\n"),
		[]byte(`{"type":"thread.started"}` + "\n"),
		[]byte("\xff\xfe binary bytes \x00 mid line"),
	}
	for _, in := range cases {
		got := a.FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want passthrough", in, got)
		}
	}
}
