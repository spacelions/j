package cursor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

const sampleListModels = `Available models

auto - Auto
composer-2-fast - Composer 2 Fast (default)
composer-2 - Composer 2
gpt-5.3-codex-low - Codex 5.3 Low
`

// fakeRunner dispatches Output by argv. It satisfies run.Runner.
type fakeRunner struct {
	handler func(name string, args []string) (string, error)
	calls   [][]string
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	cp := append([]string{name}, args...)
	f.calls = append(f.calls, cp)
	if f.handler == nil {
		return "", errors.New("fakeRunner: no handler")
	}
	return f.handler(name, args)
}

func TestAgent_Name(t *testing.T) {
	if got := New().Name(); got != "cursor" {
		t.Fatalf("Name = %q, want %q", got, "cursor")
	}
}

func TestParseModels(t *testing.T) {
	got := parseModels(sampleListModels)
	want := []string{"auto", "composer-2-fast", "composer-2", "gpt-5.3-codex-low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseModels_SkipsHeaderAndBlanks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"banner-only", "Available models\n", nil},
		{"all-blank", "\n\n  \n", nil},
		{"empty", "", nil},
		{"separator-without-id", " - Description\n", nil},
		{"trailing-blanks", "auto - Auto\n\n", []string{"auto"}},
		{"mixed", "Available models\n\nauto - Auto\nsome banner line\nfoo-bar - Foo Bar\n", []string{"auto", "foo-bar"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseModels(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListModels(t *testing.T) {
	r := &fakeRunner{handler: func(_ string, args []string) (string, error) {
		if len(args) == 1 && args[0] == "--list-models" {
			return sampleListModels, nil
		}
		return "", errors.New("unexpected")
	}}
	got, err := NewWithRunner(r).ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"auto", "composer-2-fast", "composer-2", "gpt-5.3-codex-low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(r.calls) != 1 || r.calls[0][0] != Binary {
		t.Fatalf("calls = %v", r.calls)
	}
}

func TestListModels_RunnerError(t *testing.T) {
	r := &fakeRunner{handler: func(string, []string) (string, error) { return "", errors.New("boom") }}
	if _, err := NewWithRunner(r).ListModels(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestListModels_EmptyList(t *testing.T) {
	r := &fakeRunner{handler: func(string, []string) (string, error) {
		return "Available models\n\n", nil
	}}
	_, err := NewWithRunner(r).ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no models") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckLogin(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		err     error
		wantErr bool
	}{
		{"logged-in", "Logged in as foo", nil, false},
		{"logged-in-mixed-case", "✓ login successful\nLogged In as foo", nil, false},
		{"logged-out", "Not logged in", nil, true},
		{"explicit-logged-out", "User is logged out", nil, true},
		{"not-authenticated", "Error: Not authenticated", nil, true},
		{"signed-out", "you are signed out", nil, true},
		{"unknown-output", "something unexpected", nil, true},
		{"runner-error", "", errors.New("nope"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{handler: func(string, []string) (string, error) { return tc.out, tc.err }}
			err := NewWithRunner(r).CheckLogin(context.Background())
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlan_BuildsArgsAndReturnsPlan(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	var captured []string
	r := &fakeRunner{handler: func(_ string, args []string) (string, error) {
		captured = args
		return "  1. step one\n2. step two  \n", nil
	}}
	got, err := NewWithRunner(r).Plan(context.Background(), codingagents.PlanRequest{
		TargetPath: target,
		Body:       "# task\nbody",
		Model:      "composer-2-fast",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got != "1. step one\n2. step two" {
		t.Fatalf("plan = %q", got)
	}
	want := []string{
		"--print",
		"--output-format", "text",
		"--mode", "plan",
		"--model", "composer-2-fast",
		"--workspace", dir,
	}
	if len(captured) != len(want)+1 {
		t.Fatalf("args = %v", captured)
	}
	for i, v := range want {
		if captured[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, captured[i], v)
		}
	}
	prompt := captured[len(captured)-1]
	if !strings.Contains(prompt, "# task") || !strings.Contains(prompt, target) {
		t.Fatalf("prompt missing expected substrings: %q", prompt)
	}
}

func TestPlan_EmptyOutput(t *testing.T) {
	r := &fakeRunner{handler: func(string, []string) (string, error) { return "   \n  ", nil }}
	_, err := NewWithRunner(r).Plan(context.Background(), codingagents.PlanRequest{
		TargetPath: "/tmp/x.md",
		Body:       "x",
		Model:      "m",
	})
	if err == nil {
		t.Fatal("expected empty-plan error")
	}
}

func TestPlan_RunnerError(t *testing.T) {
	r := &fakeRunner{handler: func(string, []string) (string, error) { return "", errors.New("boom") }}
	_, err := NewWithRunner(r).Plan(context.Background(), codingagents.PlanRequest{
		TargetPath: "/tmp/x.md",
		Body:       "x",
		Model:      "m",
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}
