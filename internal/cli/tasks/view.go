package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

// Viewer renders a single file at path to out (errw is wired for
// the underlying renderer's stderr). Defined as a concrete func
// type so the substitution surface stays in this package — tests
// pass a fake Viewer through *Options, mirroring Spawner.
type Viewer func(
	ctx context.Context,
	path string,
	in io.Reader,
	out, errw io.Writer,
) error

// lookPath is a package-private var defaulting to exec.LookPath so
// view tests can shadow it without introducing a configuration
// seam (per AGENTS.md: allowlist, not interface).
var lookPath = exec.LookPath

// defaultViewer is the one-shot Viewer used by `j tasks show`: bat
// (when installed and out is a TTY) -> cat (when installed) ->
// io.Copy. Wraps the exec failure with the chosen tool name + path
// so cobra surfaces a deterministic prefix.
func defaultViewer(
	ctx context.Context,
	path string,
	in io.Reader,
	out, errw io.Writer,
) error {
	tool := chooseViewerBinary(isTerminal(out))
	if tool == "" {
		return copyFileTo(path, out)
	}
	cmd := exec.CommandContext(ctx, tool, path)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errw
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %q: %w", tool, path, err)
	}
	return nil
}

// chooseViewerBinary returns "bat" when bat is on PATH AND ttyOut
// is true; "cat" when cat is on PATH; otherwise "" so the caller
// falls back to copyFileTo.
func chooseViewerBinary(ttyOut bool) string {
	if ttyOut {
		if _, err := lookPath("bat"); err == nil {
			return "bat"
		}
	}
	if _, err := lookPath("cat"); err == nil {
		return "cat"
	}
	return ""
}

// streamViewer is the production Viewer used by `j tasks logs`:
// `tail -f <path>` (when `tail` is on PATH) optionally piped
// through `tspin` (when stdout is a TTY and `tspin` is on PATH).
// When `tail` is missing, falls back to a one-shot `copyFileTo` so
// the command still surfaces the current contents on hosts without
// `tail` (e.g. Windows).
//
// A SIGINT/cancelled context is treated as a clean exit so Ctrl+C
// produces exit code 0.
func streamViewer(
	ctx context.Context,
	path string,
	in io.Reader,
	out, errw io.Writer,
) error {
	useTail, useTspin := chooseStreamMode(isTerminal(out))
	if !useTail {
		return copyFileTo(path, out)
	}
	if useTspin {
		return runTailIntoTspin(ctx, path, in, out, errw)
	}
	tailBin, err := lookPath("tail")
	if err != nil {
		return fmt.Errorf("tail %q: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, tailBin, "-f", path)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errw
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return fmt.Errorf("tail %q: %w", path, err)
	}
	return nil
}

// chooseStreamMode decides which streaming tools to use. Returns
// (useTail, useTspin):
//   - useTail is true when `tail` resolves on PATH.
//   - useTspin is true only when useTail AND ttyOut AND `tspin`
//     resolves on PATH (raw bytes when piped to a non-TTY).
func chooseStreamMode(ttyOut bool) (bool, bool) {
	if _, err := lookPath("tail"); err != nil {
		return false, false
	}
	if !ttyOut {
		return true, false
	}
	if _, err := lookPath("tspin"); err != nil {
		return true, false
	}
	return true, true
}

// runTailIntoTspin runs `tail -f <path> | tspin`, wiring tail's
// stdout to tspin's stdin and surfacing the first non-nil error
// from Wait. A cancelled ctx is reported as a clean exit. Both
// binaries are resolved through the `lookPath` allowlist seam so
// tests can shadow them deterministically.
func runTailIntoTspin(
	ctx context.Context,
	path string,
	in io.Reader,
	out, errw io.Writer,
) error {
	tailBin, err := lookPath("tail")
	if err != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, err)
	}
	tspinBin, err := lookPath("tspin")
	if err != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, err)
	}
	tail := exec.CommandContext(ctx, tailBin, "-f", path)
	tail.Stdin = in
	tail.Stderr = errw
	pipe, err := tail.StdoutPipe()
	if err != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, err)
	}
	tspin := exec.CommandContext(ctx, tspinBin)
	tspin.Stdin = pipe
	tspin.Stdout = out
	tspin.Stderr = errw
	if err := tail.Start(); err != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, err)
	}
	if err := tspin.Start(); err != nil {
		_ = tail.Process.Kill()
		_ = tail.Wait()
		return fmt.Errorf("tail|tspin %q: %w", path, err)
	}
	tailErr := tail.Wait()
	tspinErr := tspin.Wait()
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if tailErr != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, tailErr)
	}
	if tspinErr != nil {
		return fmt.Errorf("tail|tspin %q: %w", path, tspinErr)
	}
	return nil
}

// copyFileTo opens path and io.Copies its bytes to out. The fallback
// when `tail` is not available on PATH.
func copyFileTo(path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(out, f); err != nil {
		return fmt.Errorf("copy %q: %w", path, err)
	}
	return nil
}

// fileResolveOptions is the bag passed to resolveTaskFile by every
// read-only leaf (read requirements / read plan / logs / task). It
// carries exactly the fields the resolver needs so the leaves don't
// have to share a wider option type.
type fileResolveOptions struct {
	TaskID string
	UI     UI
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// resolveTaskFile centralises the --from-task vs picker decision and
// the per-file existence check shared by `read requirements`, `read
// plan`, `logs`, and `task`. Branches:
//
//   - opts.TaskID set + GetTask succeeds + file present -> (path, true, nil)
//   - opts.TaskID set + GetTask returns NotExist -> prints noTaskMessage,
//     returns ("", false, nil)
//   - opts.TaskID empty + empty store -> emptyMessage, ("", false, nil)
//   - opts.TaskID empty + picker abort -> ("", false, nil)
//   - file missing under <taskDir> -> "J: <name> not found for task <id>",
//     ("", false, nil)
//
// On every short-circuit branch the renderer is intentionally not
// invoked and exit code stays 0. Other errors propagate wrapped.
//
// The bbolt store is opened once: the same handle resolves the id
// (GetTask or pickFromStore) AND yields the per-task directory root
// via Store.Dir, so EnsureDir is unnecessary on the read paths
// (GetTask already proved the task dir exists by reading task.toml).
func resolveTaskFile(
	ctx context.Context,
	opts fileResolveOptions,
	filename string,
) (string, bool, error) {
	id, tasksDir, ok, err := openAndResolveTaskID(ctx, opts)
	if err != nil || !ok {
		return "", false, err
	}
	path := filepath.Join(tasksDir, id, filename)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			uitheme.NormalFprintf(opts.Stdout,
				"J: %s not found for task %s\n", filename, id)
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %q: %w", path, err)
	}
	return path, true, nil
}

// openAndResolveTaskID opens the bbolt store, resolves the id
// (GetTask or pickFromStore), closes the store, and returns the
// absolute tasks root so the caller can join `<root>/<id>/
// <filename>` without re-opening. The store is closed before
// returning so the file lock is released ahead of any long-running
// renderer.
func openAndResolveTaskID(
	ctx context.Context,
	opts fileResolveOptions,
) (string, string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = s.Close() }()
	if opts.TaskID != "" {
		if _, err := s.GetTask(opts.TaskID); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				uitheme.NormalFprintln(opts.Stdout, noTaskMessage)
				return "", "", false, nil
			}
			return "", "", false, err
		}
		return opts.TaskID, s.Dir(), true, nil
	}
	id, ok, err := pickFromStore(ctx, s, opts.UI, opts.Stdout)
	return id, s.Dir(), ok, err
}
