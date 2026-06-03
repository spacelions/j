package codereview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spacelions/j/internal/store/tasks"
)

// RoundsDirName is the per-task subdirectory that holds every
// code-review round. Each round lives at
// `<task-dir>/code-reviews/round-N/` where N starts at 1.
const RoundsDirName = "code-reviews"

// roundDirPrefix is the `round-` literal in `round-N/`. Centralised
// so the allocator and the test helpers share the same string.
const roundDirPrefix = "round-"

// ReviewFileName is the per-round review.toml file the github
// fetcher writes and the planner rewrites.
const ReviewFileName = "review.toml"

// PlanFileName is the per-round plan.md the planner writes. Matches
// the canonical plan.md name so future tooling can grep for either
// filename interchangeably.
const PlanFileName = "plan.md"

// ClarificationFileName is the per-round clarification.md the
// planner writes when it cannot decide a round without a human
// answer. Same filename as the canonical clarification.md so the
// round-resume logic stays simple.
const ClarificationFileName = "clarification.md"

// Round captures everything the cli needs to know about a specific
// round directory.
type Round struct {
	N                 int
	Dir               string
	ReviewTOMLPath    string
	PlanPath          string
	ClarificationPath string
}

// ResolveOrAllocate returns the round to operate on for taskID. If
// the latest round has a `clarification.md`, the same round is
// resumed in place (no new directory); otherwise the next integer
// round is created with an empty layout. The caller must hold the
// per-task flock before invoking this so two cli processes cannot
// race a new round into existence. Resumed reports whether the
// returned round resumed an existing clarification round (true) or
// allocated a fresh one (false).
func ResolveOrAllocate(taskID string) (round Round, resumed bool, err error) {
	root, err := roundsDir(taskID)
	if err != nil {
		return Round{}, false, err
	}
	rounds, err := listRoundNumbers(root)
	if err != nil {
		return Round{}, false, err
	}
	if len(rounds) > 0 {
		latest := rounds[len(rounds)-1]
		latestRound := newRoundAt(root, latest)
		if hasClarification(latestRound.ClarificationPath) {
			return latestRound, true, nil
		}
	}
	next := 1
	if len(rounds) > 0 {
		next = rounds[len(rounds)-1] + 1
	}
	allocated := newRoundAt(root, next)
	if err := os.MkdirAll(allocated.Dir, 0o755); err != nil {
		return Round{}, false, fmt.Errorf(
			"codereview: mkdir %q: %w", allocated.Dir, err)
	}
	return allocated, false, nil
}

// roundsDir returns `<task-dir>/code-reviews/`, ensuring the parent
// task dir exists. A missing task dir surfaces as a wrapped error
// so the cli can render the matching dangerous output.
func roundsDir(taskID string) (string, error) {
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return "", err
	}
	root := filepath.Join(taskDir, RoundsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("codereview: mkdir %q: %w", root, err)
	}
	return root, nil
}

// listRoundNumbers walks `<root>/round-N/` entries and returns the
// integer Ns in ascending order. Non-`round-N` entries and `round-`
// entries whose suffix is not a positive integer are skipped so a
// stray file or a future tool's directory never pollutes the
// allocator.
func listRoundNumbers(root string) ([]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("codereview: readdir %q: %w", root, err)
	}
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n, ok := parseRoundDir(e.Name())
		if !ok {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func parseRoundDir(name string) (int, bool) {
	if !strings.HasPrefix(name, roundDirPrefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, roundDirPrefix))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func newRoundAt(root string, n int) Round {
	dir := filepath.Join(root, fmt.Sprintf("%s%d", roundDirPrefix, n))
	return Round{
		N:                 n,
		Dir:               dir,
		ReviewTOMLPath:    filepath.Join(dir, ReviewFileName),
		PlanPath:          filepath.Join(dir, PlanFileName),
		ClarificationPath: filepath.Join(dir, ClarificationFileName),
	}
}

func hasClarification(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
