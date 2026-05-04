package banner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestText(t *testing.T) {
	input := "J: no tasks\nJ: work resume on task abc\n"
	got := Text(input)
	if stripped := ansi.Strip(got); stripped != input {
		t.Fatalf("ansi.Strip(Text(%q)) = %q, want %q", input, stripped, input)
	}
}

func TestDangerousText(t *testing.T) {
	input := "J: warning: tasks put: nope\nJ: reset aborted\n"
	got := DangerousText(input)
	if stripped := ansi.Strip(got); stripped != input {
		t.Fatalf("ansi.Strip(DangerousText(%q)) = %q, want %q", input, stripped, input)
	}
}

func TestFprintln(t *testing.T) {
	var buf bytes.Buffer
	Fprintln(&buf, "J: no tasks")
	if stripped := ansi.Strip(buf.String()); stripped != "J: no tasks\n" {
		t.Fatalf("ansi.Strip(Fprintln output) = %q, want %q", stripped, "J: no tasks\n")
	}
}

func TestFprintAndFprintf(t *testing.T) {
	var buf bytes.Buffer
	Fprint(&buf, "J: ", "hello")
	Fprintf(&buf, " %s", "world")
	if stripped := ansi.Strip(buf.String()); stripped != "J: hello world" {
		t.Fatalf("ansi.Strip(output) = %q", stripped)
	}
}

func TestDangerousFprintln(t *testing.T) {
	var buf bytes.Buffer
	DangerousFprintln(&buf, "J: warning: tasks put: nope")
	if stripped := ansi.Strip(buf.String()); stripped != "J: warning: tasks put: nope\n" {
		t.Fatalf("ansi.Strip(DangerousFprintln output) = %q, want %q",
			stripped, "J: warning: tasks put: nope\n")
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

// TestCannotWriteToDatabase pins the refined open-timeout banner:
// the rendered output, with ANSI styling stripped, must equal the
// literal `■ J: cannot write to database\n` so callers and tests
// can assert on the exact wording.
func TestCannotWriteToDatabase(t *testing.T) {
	var buf bytes.Buffer
	CannotWriteToDatabase(&buf)
	if stripped := ansi.Strip(buf.String()); stripped != "■ J: cannot write to database\n" {
		t.Fatalf("ansi.Strip(CannotWriteToDatabase output) = %q, want %q",
			stripped, "■ J: cannot write to database\n")
	}
}

// TestCannotWriteToDatabase_RendersDangerStyle pins the styling
// contract: with a TrueColor profile the helper must emit ANSI
// styling, must not render bold, and after stripping the styling
// the message text is unchanged.
func TestCannotWriteToDatabase_RendersDangerStyle(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	var buf bytes.Buffer
	CannotWriteToDatabase(&buf)
	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("CannotWriteToDatabase output should contain ANSI styling, got %q", out)
	}
	if hasBoldSGR(out) {
		t.Fatalf("CannotWriteToDatabase output rendered bold SGR: %q", out)
	}
	if stripped := ansi.Strip(out); stripped != "■ J: cannot write to database\n" {
		t.Fatalf("ansi.Strip(CannotWriteToDatabase output) = %q", stripped)
	}
}

func TestTextWithColorDoesNotRenderBold(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	got := Text("J: no tasks\n")
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("Text output should contain ANSI styling when color is enabled, got %q", got)
	}
	if hasBoldSGR(got) {
		t.Fatalf("Text output rendered bold SGR: %q", got)
	}
	if stripped := ansi.Strip(got); stripped != "J: no tasks\n" {
		t.Fatalf("ansi.Strip(Text output) = %q, want %q", stripped, "J: no tasks\n")
	}
}

func TestDangerousTextWithColorDoesNotRenderBold(t *testing.T) {
	restoreColorProfile(t)
	lipgloss.SetColorProfile(termenv.TrueColor)

	got := DangerousText("J: warning: tasks put: nope\n")
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("DangerousText output should contain ANSI styling when color is enabled, got %q", got)
	}
	if hasBoldSGR(got) {
		t.Fatalf("DangerousText output rendered bold SGR: %q", got)
	}
	if stripped := ansi.Strip(got); stripped != "J: warning: tasks put: nope\n" {
		t.Fatalf("ansi.Strip(DangerousText output) = %q, want %q",
			stripped, "J: warning: tasks put: nope\n")
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

// TestDisplayLogPath_FilepathRelFails covers the filepath.Rel error
// branch: on POSIX, Rel only fails when the target path cannot be
// resolved against cwd at all (e.g. a Windows-style drive prefix on
// a unix host) — so emulate the failure by chdir'ing into a relative
// cwd and passing a relative target on a different volume. Easier
// path: pass an empty absLogPath into a cwd whose Getwd succeeds;
// filepath.Rel("/x", "") resolves to "..", hitting the leading-".."
// fallback branch which is the same `return absLogPath` outcome the
// Rel-error branch produces, so we cover the "rel == empty / rel
// starts with .." escape path here too.
func TestDisplayLogPath_EmptyTargetFallsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := displayLogPath(""); got != "" {
		t.Fatalf("displayLogPath(\"\") = %q, want \"\" (fallback to absLogPath)", got)
	}
}

// TestDisplayLogPath_GetwdError covers the os.Getwd error branch.
// Strategy: chdir into a child directory and then RemoveAll the
// parent so the cwd inode is unlinked. On most Unix flavours this
// is enough to make subsequent os.Getwd calls fail with ENOENT;
// when a platform still resolves cwd from the kernel's vfs cache
// (some Darwin builds), the test falls back to a t.Skip so we
// don't block development on machines where the branch cannot be
// driven without a production seam.
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

func TestRunningInBackground(t *testing.T) {
	cwd := t.TempDir()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Chdir(resolvedCwd)
	abs := filepath.Join(resolvedCwd, ".j", "tasks", "abc", "agent.log")
	relPath := filepath.Join(".j", "tasks", "abc", "agent.log")

	var buf bytes.Buffer
	RunningInBackground(&buf, "task abc", 12345, abs)
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

	// Second row must be a blank text row (no banner glyph) sitting
	// between the two text rows. The lipgloss border draws side
	// glyphs on every row, so we expect three content rows separated
	// by `│ ... │` framing.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("rendered banner = %d lines, want >=5\n%s", len(lines), out)
	}
	subjectIdx, blankIdx, tailIdx := -1, -1, -1
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
	blankIdx = subjectIdx + 1
	blank := lines[blankIdx]
	stripped := strings.Trim(blank, "│ \t")
	if stripped != "" {
		t.Fatalf("middle row should be blank between borders, got %q", blank)
	}
}

func TestRunningInBackgroundDoesNotRenderBold(t *testing.T) {
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
	RunningInBackground(&buf, "task abc", 12345, abs)
	out := buf.String()
	if hasBoldSGR(out) {
		t.Fatalf("RunningInBackground output rendered bold SGR: %q", out)
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
