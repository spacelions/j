package uitheme

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestNormalText(t *testing.T) {
	input := "J: no tasks\nJ: work resume on task abc\n"
	got := NormalText(input)
	if stripped := ansi.Strip(got); stripped != input {
		t.Fatalf("ansi.Strip(NormalText(%q)) = %q, want %q", input, stripped, input)
	}
}

func TestDangerousText(t *testing.T) {
	input := "J: tasks put: nope\nJ: reset aborted\n"
	got := DangerousText(input)
	if stripped := ansi.Strip(got); stripped != input {
		t.Fatalf("ansi.Strip(DangerousText(%q)) = %q, want %q", input, stripped, input)
	}
}

func TestNormalFprintln(t *testing.T) {
	var buf bytes.Buffer
	NormalFprintln(&buf, "J: no tasks")
	if stripped := ansi.Strip(buf.String()); stripped != "J: no tasks\n" {
		t.Fatalf("ansi.Strip(NormalFprintln output) = %q, want %q", stripped, "J: no tasks\n")
	}
}

func TestNormalFprintAndFprintf(t *testing.T) {
	var buf bytes.Buffer
	NormalFprint(&buf, "J: ", "hello")
	NormalFprintf(&buf, " %s", "world")
	if stripped := ansi.Strip(buf.String()); stripped != "J: hello world" {
		t.Fatalf("ansi.Strip(output) = %q", stripped)
	}
}

func TestDangerousFprintln(t *testing.T) {
	var buf bytes.Buffer
	DangerousFprintln(&buf, "J: tasks put: nope")
	if stripped := ansi.Strip(buf.String()); stripped != "J: tasks put: nope\n" {
		t.Fatalf("ansi.Strip(DangerousFprintln output) = %q, want %q",
			stripped, "J: tasks put: nope\n")
	}
}

func TestDangerousDialogBox(t *testing.T) {
	var buf bytes.Buffer
	DangerousDialogBox(&buf, "J: tasks db: %v", errors.New("boom"))
	stripped := ansi.Strip(buf.String())
	if !strings.Contains(stripped, "J: tasks db: boom") {
		t.Fatalf("output missing the formatted body: %q", stripped)
	}
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if !strings.Contains(stripped, glyph) {
			t.Fatalf("output missing border glyph %q: %q", glyph, stripped)
		}
	}
}

func TestDangerousFprintAndFprintf(t *testing.T) {
	var buf bytes.Buffer
	DangerousFprint(&buf, "J: ", "warning")
	DangerousFprintf(&buf, ": %s", "boom")
	if stripped := ansi.Strip(buf.String()); stripped != "J: warning: boom" {
		t.Fatalf("ansi.Strip(output) = %q", stripped)
	}
}

func TestNormalTextWithColorDoesNotRenderBold(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	got := NormalText("J: no tasks\n")
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("NormalText output should contain ANSI styling when color is enabled, got %q", got)
	}
	if hasBoldSGR(got) {
		t.Fatalf("NormalText output rendered bold SGR: %q", got)
	}
	if stripped := ansi.Strip(got); stripped != "J: no tasks\n" {
		t.Fatalf("ansi.Strip(NormalText output) = %q, want %q", stripped, "J: no tasks\n")
	}
}

func TestDangerousTextWithColorDoesNotRenderBold(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	got := DangerousText("J: tasks put: nope\n")
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("DangerousText output should contain ANSI styling when color is enabled, got %q", got)
	}
	if hasBoldSGR(got) {
		t.Fatalf("DangerousText output rendered bold SGR: %q", got)
	}
	if stripped := ansi.Strip(got); stripped != "J: tasks put: nope\n" {
		t.Fatalf("ansi.Strip(DangerousText output) = %q, want %q",
			stripped, "J: tasks put: nope\n")
	}
}

func TestDisplayLogPath(t *testing.T) {
	cwd := t.TempDir()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("EvalSymlinks(cwd): %v", err)
	}
	outside := t.TempDir()
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(outside): %v", err)
	}

	cases := []struct {
		name string
		abs  string
		want string
	}{
		{
			name: "inside_cwd_returns_relative",
			abs:  filepath.Join(resolvedCwd, ".j", "tasks", "abc", "agent.log"),
			want: filepath.Join(".j", "tasks", "abc", "agent.log"),
		},
		{
			name: "outside_cwd_falls_back_to_absolute",
			abs:  filepath.Join(resolvedOutside, "agent.log"),
			want: filepath.Join(resolvedOutside, "agent.log"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(resolvedCwd)
			if got := displayLogPath(tc.abs); got != tc.want {
				t.Fatalf("displayLogPath(%q) = %q, want %q", tc.abs, got, tc.want)
			}
		})
	}
}

func TestDisplayLogPath_EmptyTargetFallsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := displayLogPath(""); got != "" {
		t.Fatalf("displayLogPath(\"\") = %q, want \"\" (fallback to absLogPath)", got)
	}
}

func TestDisplayLogPath_GetwdError(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(child); err != nil {
		t.Fatalf("Chdir(child): %v", err)
	}
	t.Setenv("PWD", "")
	if err := os.RemoveAll(parent); err != nil {
		t.Skipf("RemoveAll(parent) failed; cannot drive Getwd error: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd still succeeds after the cwd was removed; cannot drive this branch on this platform")
	}
	abs := "/var/log/agent.log"
	if got := displayLogPath(abs); got != abs {
		t.Fatalf("displayLogPath(%q) = %q, want absolute fallback", abs, got)
	}
}

func TestNormalForkDialog(t *testing.T) {
	cwd := t.TempDir()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Chdir(resolvedCwd)
	abs := filepath.Join(resolvedCwd, ".j", "tasks", "abc", "agent.log")
	relPath := filepath.Join(".j", "tasks", "abc", "agent.log")

	var buf bytes.Buffer
	NormalForkDialog(&buf, "task abc", 12345, abs)
	out := buf.String()

	wantSubstrings := []string{
		"┌",
		"└",
		"J: task abc running in background (PID=12345)",
		"tail -f " + relPath,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\n---\n%s", s, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("rendered banner = %d lines, want >=5\n%s", len(lines), out)
	}
	subjectIdx, tailIdx := -1, -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "running in background"):
			subjectIdx = i
		case strings.Contains(line, "tail -f"):
			tailIdx = i
		}
	}
	if subjectIdx < 0 || tailIdx < 0 || tailIdx-subjectIdx != 2 {
		t.Fatalf("expected exactly one blank row between subject (idx=%d) and tail (idx=%d)\n%s",
			subjectIdx, tailIdx, out)
	}
	blankIdx := subjectIdx + 1
	blank := lines[blankIdx]
	stripped := strings.Trim(blank, "│ \t")
	if stripped != "" {
		t.Fatalf("middle row should be blank between borders, got %q", blank)
	}
}

func TestNormalForkDialogDoesNotRenderBold(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	cwd := t.TempDir()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Chdir(resolvedCwd)
	abs := filepath.Join(resolvedCwd, ".j", "tasks", "abc", "agent.log")

	var buf bytes.Buffer
	NormalForkDialog(&buf, "task abc", 12345, abs)
	out := buf.String()
	if hasBoldSGR(out) {
		t.Fatalf("NormalForkDialog output rendered bold SGR: %q", out)
	}
	if stripped := ansi.Strip(out); !strings.Contains(stripped, "J: task abc running in background (PID=12345)") {
		t.Fatalf("stripped output missing subject row: %q", stripped)
	}
}

func restoreColorProfile(t *testing.T) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
	})
}

func hasBoldSGR(s string) bool {
	for len(s) > 0 {
		idx := strings.Index(s, "\x1b[")
		if idx < 0 {
			return false
		}
		s = s[idx+2:]
		end := strings.IndexByte(s, 'm')
		if end < 0 {
			return false
		}
		for _, param := range strings.Split(s[:end], ";") {
			if param == "1" {
				return true
			}
		}
		s = s[end+1:]
	}
	return false
}
