package tasks

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

// codeReviewsDirName is the per-task subdirectory that holds every
// code-review round. Each round lives at
// `<task-dir>/code-reviews/round-N/` where N starts at 1.
const codeReviewsDirName = "code-reviews"

// roundDirPrefix is the `round-` literal in `round-N/`. Centralised
// so the allocator and the test helpers share the same string.
const roundDirPrefix = "round-"

// reviewTOMLFileName is the per-round review.toml file the github
// fetcher writes and the planner rewrites.
const reviewTOMLFileName = "review.toml"

// roundPlanFileName is the per-round plan.md the planner writes.
// Matches the canonical plan.md name so future tooling can grep for
// either filename interchangeably.
const roundPlanFileName = "plan.md"

// roundClarificationFileName is the per-round clarification.md the
// planner writes when it cannot decide a round without a human
// answer. Same filename as the canonical clarification.md so the
// round-resume logic stays simple.
const roundClarificationFileName = "clarification.md"

// codeReviewRound captures everything the cli needs to know about a
// specific round directory.
type codeReviewRound struct {
	N                 int
	Dir               string
	ReviewTOMLPath    string
	PlanPath          string
	ClarificationPath string
}

// resolveOrAllocateRound returns the round to operate on for taskID.
// If the latest round has a `clarification.md`, the same round is
// resumed in place (no new directory); otherwise the next integer
// round is created with an empty layout. The caller must hold the
// per-task flock before invoking this so two cli processes cannot
// race a new round into existence.
func resolveOrAllocateRound(taskID string) (codeReviewRound, error) {
	root, err := codeReviewsDir(taskID)
	if err != nil {
		return codeReviewRound{}, err
	}
	rounds, err := listRoundNumbers(root)
	if err != nil {
		return codeReviewRound{}, err
	}
	if len(rounds) > 0 {
		latest := rounds[len(rounds)-1]
		latestRound := newRoundAt(root, latest)
		if hasClarification(latestRound.ClarificationPath) {
			return latestRound, nil
		}
	}
	next := 1
	if len(rounds) > 0 {
		next = rounds[len(rounds)-1] + 1
	}
	round := newRoundAt(root, next)
	if err := os.MkdirAll(round.Dir, 0o755); err != nil {
		return codeReviewRound{}, fmt.Errorf(
			"code-review: mkdir %q: %w", round.Dir, err)
	}
	return round, nil
}

// codeReviewsDir returns `<task-dir>/code-reviews/`, ensuring the
// parent task dir exists. A missing task dir surfaces as a wrapped
// error so the cli can render the matching dangerous dialog.
func codeReviewsDir(taskID string) (string, error) {
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return "", err
	}
	root := filepath.Join(taskDir, codeReviewsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("code-review: mkdir %q: %w", root, err)
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
		return nil, fmt.Errorf("code-review: readdir %q: %w", root, err)
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

func newRoundAt(root string, n int) codeReviewRound {
	dir := filepath.Join(root, fmt.Sprintf("%s%d", roundDirPrefix, n))
	return codeReviewRound{
		N:                 n,
		Dir:               dir,
		ReviewTOMLPath:    filepath.Join(dir, reviewTOMLFileName),
		PlanPath:          filepath.Join(dir, roundPlanFileName),
		ClarificationPath: filepath.Join(dir, roundClarificationFileName),
	}
}

func hasClarification(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
