package tasks

import (
	"fmt"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// taskFile is the on-disk shape of task.toml. Sections are ordered the
// same way the file should appear: metadata first, then planner /
// worker / verifier phase data, then external integration links, and
// finally log discovery. Optional string fields use omitempty so empty
// integration data does not show up as populated keys. Time fields use
// value time.Time without omitempty because pelletier/go-toml/v2 has
// the bugs documented in wire_test.go.
type taskFile struct {
	Metadata taskMetadataSection `toml:"metadata"`
	Planner  taskPhaseSection    `toml:"planner"`
	Worker   taskWorkerSection   `toml:"worker"`
	Verifier taskPhaseSection    `toml:"verifier"`
	Linear   taskLinearSection   `toml:"linear"`
	GitHub   taskGitHubSection   `toml:"github"`
	Logs     taskLogsSection     `toml:"logs"`
}

// taskMetadataSection holds the task-level identifier, lifecycle
// status, human summary, and terminal done_at timestamp.
type taskMetadataSection struct {
	ID      string     `toml:"id"`
	Status  TaskStatus `toml:"status"`
	Summary string     `toml:"summary"`
	DoneAt  time.Time  `toml:"done_at"`
}

// taskPhaseSection is the shared shape used by the planner and
// verifier phase tables. Each phase records its selected tool/model,
// the resume token minted by the agent, and the begin/end timestamps.
type taskPhaseSection struct {
	Tool          string    `toml:"tool,omitempty"`
	Model         string    `toml:"model,omitempty"`
	ResumeSession string    `toml:"resume_session,omitempty"`
	BeginAt       time.Time `toml:"begin_at"`
	EndAt         time.Time `toml:"end_at"`
}

// taskWorkerSection is the worker phase shape. It adds the worktree
// name that the worker mints on first run and the verifier reuses; the
// rest mirrors taskPhaseSection.
type taskWorkerSection struct {
	Tool          string    `toml:"tool,omitempty"`
	Model         string    `toml:"model,omitempty"`
	ResumeSession string    `toml:"resume_session,omitempty"`
	BeginAt       time.Time `toml:"begin_at"`
	EndAt         time.Time `toml:"end_at"`
	Worktree      string    `toml:"worktree,omitempty"`
}

// taskLinearSection is the Linear integration link grouping.
type taskLinearSection struct {
	Issue string `toml:"issue,omitempty"`
}

// taskGitHubSection is the GitHub integration link grouping.
type taskGitHubSection struct {
	PullRequestURL string `toml:"pull_request_url,omitempty"`
}

// taskLogsSection is the log discovery grouping.
type taskLogsSection struct {
	AgentLogPath string `toml:"agent_log_path,omitempty"`
}

// taskFileFromTask projects a Task into the sectioned wire shape so
// PutTask can write the new layout without changing the in-memory API.
func taskFileFromTask(t Task) taskFile {
	return taskFile{
		Metadata: taskMetadataSection{
			ID:      t.ID,
			Status:  t.Status,
			Summary: t.Summary,
			DoneAt:  t.DoneAt,
		},
		Planner: taskPhaseSection{
			Tool:          t.PlanTool,
			Model:         t.PlanModel,
			ResumeSession: t.PlanResumeSession,
			BeginAt:       t.PlanBeginAt,
			EndAt:         t.PlanEndAt,
		},
		Worker: taskWorkerSection{
			Tool:          t.WorkTool,
			Model:         t.WorkModel,
			ResumeSession: t.WorkResumeSession,
			BeginAt:       t.WorkBeginAt,
			EndAt:         t.WorkEndAt,
			Worktree:      t.Worktree,
		},
		Verifier: taskPhaseSection{
			Tool:          t.VerifyTool,
			Model:         t.VerifyModel,
			ResumeSession: t.VerifyResumeSession,
			BeginAt:       t.VerifyBeginAt,
			EndAt:         t.VerifyEndAt,
		},
		Linear: taskLinearSection{Issue: t.LinearIssue},
		GitHub: taskGitHubSection{PullRequestURL: t.PullRequestURL},
		Logs:   taskLogsSection{AgentLogPath: t.AgentLogPath},
	}
}

// taskFromFile reverses taskFileFromTask so GetTask/ListTasks can hand
// callers a flat Task value.
func taskFromFile(f taskFile) Task {
	return Task{
		ID:                  f.Metadata.ID,
		Status:              f.Metadata.Status,
		Summary:             f.Metadata.Summary,
		DoneAt:              f.Metadata.DoneAt,
		PlanTool:            f.Planner.Tool,
		PlanModel:           f.Planner.Model,
		PlanResumeSession:   f.Planner.ResumeSession,
		PlanBeginAt:         f.Planner.BeginAt,
		PlanEndAt:           f.Planner.EndAt,
		WorkTool:            f.Worker.Tool,
		WorkModel:           f.Worker.Model,
		WorkResumeSession:   f.Worker.ResumeSession,
		WorkBeginAt:         f.Worker.BeginAt,
		WorkEndAt:           f.Worker.EndAt,
		Worktree:            f.Worker.Worktree,
		VerifyTool:          f.Verifier.Tool,
		VerifyModel:         f.Verifier.Model,
		VerifyResumeSession: f.Verifier.ResumeSession,
		VerifyBeginAt:       f.Verifier.BeginAt,
		VerifyEndAt:         f.Verifier.EndAt,
		LinearIssue:         f.Linear.Issue,
		PullRequestURL:      f.GitHub.PullRequestURL,
		AgentLogPath:        f.Logs.AgentLogPath,
	}
}

// marshalTask encodes t as a sectioned task.toml document.
func marshalTask(t Task) ([]byte, error) {
	return toml.Marshal(taskFileFromTask(t))
}

// unmarshalTask decodes data into a Task, accepting both the new
// sectioned layout and the historical flat layout. Files written
// before this change have all keys at the document root; new writes
// use the sectioned shape. The legacy fallback only fires when the
// sectioned metadata table is absent so existing rows continue to
// load through GetTask/ListTasks unchanged.
func unmarshalTask(data []byte, id string) (Task, error) {
	var f taskFile
	if err := toml.Unmarshal(data, &f); err == nil &&
		f.Metadata.ID != "" {
		return taskFromFile(f), nil
	}
	var flat Task
	if err := toml.Unmarshal(data, &flat); err != nil {
		return Task{}, fmt.Errorf(
			"store: decode task %q: %w", id, err)
	}
	return flat, nil
}
