package resolver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/codereview"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/github"
)

// CodeReviewPickUI is the picker surface ResolveCodeReviewTaskID
// drives when no --from-task is supplied. *picker.Picker satisfies
// it; tests inject scripted fakes.
type CodeReviewPickUI interface {
	PickTask(
		ctx context.Context, tasks []tasks.Task,
	) (string, bool, error)
}

// ResolveCodeReviewTaskID returns the id of the task the next
// code-review round targets. With a non-empty fromTask it short-
// circuits to that id; otherwise it lists every task with a stored
// PullRequestURL, sorts via tasks.SortTasks, and drives the picker.
// An empty eligible set surfaces a dangerous output and a typed
// error so the cli renders the same wording every time.
func ResolveCodeReviewTaskID(
	ctx context.Context, ui CodeReviewPickUI,
	stderr io.Writer, fromTask string,
) (string, bool, error) {
	if fromTask != "" {
		return fromTask, true, nil
	}
	rows, err := ListTasksWithPR()
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		uitheme.DangerousOutput(stderr,
			"J: no tasks with a stored pull request URL; "+
				"run `j tasks start` and let the worker open a PR first")
		return "", false, errors.New("code-review: no eligible tasks")
	}
	tasks.SortTasks(rows)
	id, ok, err := ui.PickTask(ctx, rows)
	if err != nil {
		return "", false, err
	}
	return id, ok, nil
}

// ListTasksWithPR returns every task row with a non-empty
// PullRequestURL, in the order ListTasks returned them. The store
// is opened and closed inside the call so the file lock is never
// held across the picker.
func ListTasksWithPR() ([]tasks.Task, error) {
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	out := make([]tasks.Task, 0, len(all))
	for _, t := range all {
		if t.PullRequestURL == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// ResolvePlannerAgent reads the planner bucket from the project
// settings store and returns the matching agent + model. The store
// is closed before returning so the bbolt file lock is not held
// across the long planner round-trip.
func ResolvePlannerAgent(
	ctx context.Context, agents []codingagents.Agent, stderr io.Writer,
) (codingagents.Agent, string, error) {
	s, ok := store.OpenSettings(stderr)
	if !ok {
		return nil, "", ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return AgentFromStore(ctx, s, store.BucketPlanner, agents)
}

// CodeReviewFetcher narrows the github.Client surface
// FetchAndWriteReview consumes. Production wiring passes a real
// github.Client; tests inject a scripted fetcher.
type CodeReviewFetcher interface {
	FetchPR(
		ctx context.Context, ref github.PRRef,
	) (github.FetchResult, error)
}

// FetchAndWriteReview runs the GitHub GraphQL fetch, projects the
// result into a codereview.ReviewFile, writes it to the round's
// review.toml, and returns the snapshot of fetched source_ids the
// validator expects to find after the planner runs. The round's
// directory must already exist (codereview.ResolveOrAllocate
// creates it).
func FetchAndWriteReview(
	ctx context.Context,
	fetcher CodeReviewFetcher,
	round codereview.Round,
	ref github.PRRef,
	stderr io.Writer,
) (codereview.SourceIDSet, error) {
	res, err := fetcher.FetchPR(ctx, ref)
	if err != nil {
		uitheme.DangerousOutput(stderr, "J: %v", err)
		return nil, err
	}
	file := codereview.ReviewFile{
		SchemaVersion: codereview.SchemaVersion,
		Provider:      "github",
		FetchedAt:     time.Now().UTC(),
		PR: codereview.PR{
			URL:    res.PR.URL,
			Owner:  res.PR.Owner,
			Repo:   res.PR.Repo,
			Number: res.PR.Number,
			State:  res.PR.State,
			Draft:  res.PR.Draft,
			Merged: res.PR.Merged,
		},
		Items: itemsFromFetch(res.Items),
	}
	if err := codereview.Save(round.ReviewTOMLPath, file); err != nil {
		return nil, err
	}
	return codereview.SnapshotSourceIDs(file), nil
}

func itemsFromFetch(in []github.Item) []codereview.Item {
	out := make([]codereview.Item, 0, len(in))
	for _, it := range in {
		out = append(out, codereview.Item{
			SourceID:   it.SourceID,
			Kind:       string(it.Kind),
			ThreadID:   it.ThreadID,
			Author:     it.Author,
			Body:       it.Body,
			Path:       it.Path,
			Line:       it.Line,
			IsOutdated: it.IsOutdated,
			HasJReply:  it.HasJReply,
		})
	}
	return out
}

// ValidateReviewRound loads the planner-updated review.toml and
// runs codereview.ValidateRound. Surface errors are written as
// dangerous output so the cli does not have to re-wrap them.
func ValidateReviewRound(
	round codereview.Round,
	ids codereview.SourceIDSet,
	stderr io.Writer,
) error {
	file, err := codereview.Load(round.ReviewTOMLPath)
	if err != nil {
		uitheme.DangerousOutput(stderr, "J: %v", err)
		return err
	}
	if err := codereview.ValidateRound(file, ids, round.PlanPath); err != nil {
		uitheme.DangerousOutput(stderr, "J: %v", err)
		return err
	}
	return nil
}

// CodeReviewTaskStatusError is returned by GuardCodeReviewTask when
// the resolved task is in a status that overlaps the planner /
// worker / verifier flock. The cli uses errors.As to surface a
// dangerous output without re-wording the message.
type CodeReviewTaskStatusError struct {
	TaskID string
	Status tasks.TaskStatus
}

func (e *CodeReviewTaskStatusError) Error() string {
	return fmt.Sprintf(
		"code-review: task %s status %q forbids review",
		e.TaskID, e.Status)
}

// codeReviewDisallowedStatuses are the lifecycle states the parent
// rejects before spawning the child. Mid-flight tasks share their
// flock with the orchestrator and the code-review command must not
// race them.
var codeReviewDisallowedStatuses = map[tasks.TaskStatus]bool{
	tasks.StatusPlanning:  true,
	tasks.StatusWorking:   true,
	tasks.StatusVerifying: true,
}

// GuardCodeReviewTask validates the resolved task row before any
// fetch or planner work. Tasks without a PR URL and tasks in
// planning / working / verifying are rejected with the matching
// dangerous output.
func GuardCodeReviewTask(stderr io.Writer, row tasks.Task) error {
	if row.PullRequestURL == "" {
		uitheme.DangerousOutput(stderr,
			"J: task %s has no PullRequestURL; "+
				"set it via the worker turn first", row.ID)
		return fmt.Errorf("code-review: task %s has no PR URL", row.ID)
	}
	if codeReviewDisallowedStatuses[row.Status] {
		uitheme.DangerousOutput(stderr,
			"J: task %s is %s; wait for the orchestrator "+
				"to finish before reviewing", row.ID, row.Status)
		return &CodeReviewTaskStatusError{TaskID: row.ID, Status: row.Status}
	}
	return nil
}

// PickerCodeReviewUI adapts *picker.Picker to CodeReviewPickUI by
// forwarding through the existing PickTask helper. Lets cli wiring
// pass a picker without re-implementing the interface dance.
type PickerCodeReviewUI struct {
	*picker.Picker
}

// PickTask satisfies CodeReviewPickUI.
func (p PickerCodeReviewUI) PickTask(
	ctx context.Context, rows []tasks.Task,
) (string, bool, error) {
	return p.Picker.PickTask(ctx, "Select a task", rows)
}
