package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/spacelions/j/internal/cli/banner"
)

// Sentinel errors returned by LoadProjectConfig when a required
// project setting is missing or unreadable. Each message tells the
// user the exact `j settings set project.<key>=<value>` command to
// run.
var (
	ErrMissingAPIKey        = errors.New("project.api_key is not set; run `j settings set project.api_key=<your-google-api-key>`")
	ErrMissingModel         = errors.New("project.model is not set; run `j settings set project.model=<gemini-model>` (e.g. gemini-2.5-flash)")
	ErrMissingMaxIterations = errors.New("project.max_iterations is not set or is invalid; run `j settings set project.max_iterations=<positive-integer>`")
)

// DefaultTaskMaxIterations is the fallback applied when
// `project.max_iterations` is unset, unparseable, or zero. Exported
// so the verifier shell-out agent and the `j verify` cobra flag
// share a single source of truth.
const DefaultTaskMaxIterations = 3

// Project bucket keys shared by init, settings loaders, and tests.
const (
	KeyMaxIterations        = "max_iterations"
	KeyPlanRequiresApproval = "plan_requires_approval"
)

// DefaultPlanRequiresApproval is the task-orchestrator gate default.
// Fresh projects pause after planning unless the user opts out.
const DefaultPlanRequiresApproval = true

// ProjectConfig bundles the runtime knobs `j run` and `j web` read
// from the per-project settings store. Lives in the store package so
// callers don't drag in an unrelated import path just to construct
// one for tests.
type ProjectConfig struct {
	// APIKey is the Gemini / Google API key.
	APIKey string
	// Model is the Gemini model name (e.g. "gemini-2.5-flash").
	Model string
	// MaxIterations bounds the worker/verifier loop.
	MaxIterations uint
}

// TaskConfig is the relaxed runtime config consumed by `j tasks
// orchestrate`. Only MaxIterations is meaningful; the Gemini knobs
// LoadProjectConfig demands (`project.api_key`, `project.model`)
// are intentionally absent because the shell-out path never
// instantiates a Gemini model — the actual LLM calls happen inside
// the cursor / claude binaries that the per-phase machinery spawns.
type TaskConfig struct {
	// MaxIterations bounds the verifier's internal worker→verifier
	// fix loop. Defaults to DefaultTaskMaxIterations (3) when the
	// project setting is unset / unparseable / zero.
	MaxIterations int
}

// OpenSettings opens `<cwd>/.j/settings` and returns the store
// together with a success flag. Pre-flight (`j init`) has already
// laid the layout down, so any failure here is real (e.g. concurrent
// locks) and surfaces as a single "warning: ..." line on stderr;
// the caller should short-circuit without panicking. Both `j plan`
// and `j work` (and the rest of the cli) use this so the open-and-
// warn pattern is uniform.
//
// Callers own the bbolt handle on success. Best-effort writers like
// PersistAgentSelection close the store inside the call so the file
// lock is not held across long-running agent invocations.
func OpenSettings(stderr io.Writer) (*Store, bool) {
	path, err := DefaultPath()
	if err != nil {
		banner.DangerousBox(stderr, "J: settings path: %v", err)
		return nil, false
	}
	s, err := Open(path)
	if err != nil {
		banner.DangerousBox(stderr, "J: settings db: %v", err)
		return nil, false
	}
	return s, true
}

// LoadProjectConfig reads the three runtime knobs from the per-
// project bbolt settings store at `<cwd>/.j/settings`. A missing
// file surfaces a wrapped fs.ErrNotExist error suggesting `j init`;
// a missing or unparseable value surfaces the matching sentinel
// above (ErrMissingAPIKey / ErrMissingModel / ErrMissingMaxIterations).
func LoadProjectConfig() (ProjectConfig, error) {
	path, err := DefaultPath()
	if err != nil {
		return ProjectConfig{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ProjectConfig{}, fmt.Errorf("store: settings store missing at %q; run `j init`: %w", path, err)
		}
		return ProjectConfig{}, fmt.Errorf("store: stat %q: %w", path, err)
	}

	s, err := Open(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("store: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()

	apiKey, err := readSetting(s, "api_key")
	if err != nil {
		return ProjectConfig{}, err
	}
	if apiKey == "" {
		return ProjectConfig{}, ErrMissingAPIKey
	}
	model, err := readSetting(s, "model")
	if err != nil {
		return ProjectConfig{}, err
	}
	if model == "" {
		return ProjectConfig{}, ErrMissingModel
	}
	rawIters, err := readSetting(s, KeyMaxIterations)
	if err != nil {
		return ProjectConfig{}, err
	}
	if rawIters == "" {
		return ProjectConfig{}, ErrMissingMaxIterations
	}
	n, err := strconv.ParseUint(rawIters, 10, 64)
	if err != nil || n == 0 {
		return ProjectConfig{}, ErrMissingMaxIterations
	}

	return ProjectConfig{APIKey: apiKey, Model: model, MaxIterations: uint(n)}, nil
}

// LoadTaskConfig reads only `project.max_iterations` from the
// per-project bbolt settings store. Missing file or missing key
// surface as the documented default (DefaultTaskMaxIterations) so a
// fresh project can run `j tasks start` end to end without setting
// any project knobs. A genuine bbolt open / read error still surfaces
// verbatim; only the "no settings yet" / "no value yet" cases are
// silently defaulted.
func LoadTaskConfig() (TaskConfig, error) {
	cfg := TaskConfig{MaxIterations: DefaultTaskMaxIterations}
	path, err := DefaultPath()
	if err != nil {
		return TaskConfig{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return TaskConfig{}, fmt.Errorf("store: stat %q: %w", path, err)
	}
	s, err := Open(path)
	if err != nil {
		return TaskConfig{}, fmt.Errorf("store: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()
	raw, err := readSetting(s, KeyMaxIterations)
	if err != nil {
		return TaskConfig{}, err
	}
	if raw == "" {
		return cfg, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 {
		return cfg, nil
	}
	cfg.MaxIterations = int(n)
	return cfg, nil
}

// LoadPlanRequiresApproval reads `project.plan_requires_approval`.
// Missing settings, missing keys, and unparseable values all fall
// back to DefaultPlanRequiresApproval so stale project settings never
// block the detached task orchestrator.
func LoadPlanRequiresApproval() (bool, error) {
	path, err := DefaultPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultPlanRequiresApproval, nil
		}
		return false, fmt.Errorf("store: stat %q: %w", path, err)
	}
	s, err := Open(path)
	if err != nil {
		return false, fmt.Errorf("store: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()
	raw, err := readSetting(s, KeyPlanRequiresApproval)
	if err != nil {
		return false, err
	}
	if raw == "" {
		return DefaultPlanRequiresApproval, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return DefaultPlanRequiresApproval, nil
	}
	return v, nil
}

// PersistAgentSelection writes tool/model/interactive into bucket as
// a best-effort operation: any error is logged to stderr as a single
// "warning: persist <key>: ..." line and the function returns. A nil
// store is a silent no-op so callers can pipe the value straight from
// Options.Store without nil-checks. Plan / work / verify all use this
// so the on-disk schema is identical.
func PersistAgentSelection(s *Store, stderr io.Writer, bucket, tool, model string, interactive bool) {
	if s == nil {
		return
	}
	entries := [][2]string{
		{"tool", tool},
		{"model", model},
		{"interactive", strconv.FormatBool(interactive)},
	}
	for _, kv := range entries {
		if err := s.Put(bucket, kv[0], kv[1]); err != nil {
			banner.DangerousBox(stderr, "J: persist %s: %v", kv[0], err)
			return
		}
	}
}

// readSetting reads a single key from the project bucket. Whitespace
// is trimmed so a stray space stored via `j settings set` doesn't
// silently defeat the missing-value check.
func readSetting(s *Store, key string) (string, error) {
	v, _, err := s.Get(BucketProject, key)
	if err != nil {
		return "", fmt.Errorf("store: read project.%s: %w", key, err)
	}
	return strings.TrimSpace(v), nil
}
