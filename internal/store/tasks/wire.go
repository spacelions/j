package tasks

import "time"

// taskWire is the on-disk projection of Task for TOML serialisation.
// pelletier/go-toml/v2 has a known asymmetry where `*time.Time`
// encodes as a quoted TOML string (literal) instead of a TOML
// datetime, and the resulting file does not round-trip. Using
// `time.Time` (value) sidesteps that bug, at the cost of always
// rendering the field even when unset; we use the zero value
// (`0001-01-01T00:00:00Z`) as the "not set" sentinel and translate
// to/from `*time.Time` at the encode/decode boundary so the in-memory
// Task API stays unchanged.
type taskWire struct {
	ID                 string     `toml:"id"`
	Status             TaskStatus `toml:"status"`
	InvokedTool        string     `toml:"invoked_tool"`
	InvokedModel       string     `toml:"invoked_model"`
	Worktree           string     `toml:"worktree"`
	Summary            string     `toml:"summary"`
	PlanResumeCursor   string     `toml:"plan_resume_cursor"`
	WorkResumeCursor   string     `toml:"work_resume_cursor"`
	VerifyResumeCursor string     `toml:"verify_resume_cursor"`
	PlanBeginAt        time.Time  `toml:"plan_begin_at"`
	PlanEndAt          time.Time  `toml:"plan_end_at"`
	WorkBeginAt        time.Time  `toml:"work_begin_at"`
	WorkEndAt          time.Time  `toml:"work_end_at"`
	VerifyBeginAt      time.Time  `toml:"verify_begin_at"`
	VerifyEndAt        time.Time  `toml:"verify_end_at"`
	DoneAt             time.Time  `toml:"done_at"`
	BackgroundPID      int        `toml:"background_pid"`
	AgentLogPath       string     `toml:"agent_log_path"`
	LinearIssue        string     `toml:"linear_issue"`
}

// derefTime returns *p, or the zero time when p is nil.
func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// optTimePtr returns a pointer to a copy of v, or nil when v is the
// zero time (our "not set" sentinel).
func optTimePtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	cp := v
	return &cp
}

func taskToWire(t Task) taskWire {
	return taskWire{
		ID: t.ID, Status: t.Status,
		InvokedTool: t.InvokedTool, InvokedModel: t.InvokedModel,
		Worktree: t.Worktree, Summary: t.Summary,
		PlanResumeCursor:   t.PlanResumeCursor,
		WorkResumeCursor:   t.WorkResumeCursor,
		VerifyResumeCursor: t.VerifyResumeCursor,
		PlanBeginAt:        derefTime(t.PlanBeginAt),
		PlanEndAt:          derefTime(t.PlanEndAt),
		WorkBeginAt:        derefTime(t.WorkBeginAt),
		WorkEndAt:          derefTime(t.WorkEndAt),
		VerifyBeginAt:      derefTime(t.VerifyBeginAt),
		VerifyEndAt:        derefTime(t.VerifyEndAt),
		DoneAt:             derefTime(t.DoneAt),
		BackgroundPID:      t.BackgroundPID,
		AgentLogPath:       t.AgentLogPath,
		LinearIssue:        t.LinearIssue,
	}
}

func wireToTask(w taskWire) Task {
	return Task{
		ID: w.ID, Status: w.Status,
		InvokedTool: w.InvokedTool, InvokedModel: w.InvokedModel,
		Worktree: w.Worktree, Summary: w.Summary,
		PlanResumeCursor:   w.PlanResumeCursor,
		WorkResumeCursor:   w.WorkResumeCursor,
		VerifyResumeCursor: w.VerifyResumeCursor,
		PlanBeginAt:        optTimePtr(w.PlanBeginAt),
		PlanEndAt:          optTimePtr(w.PlanEndAt),
		WorkBeginAt:        optTimePtr(w.WorkBeginAt),
		WorkEndAt:          optTimePtr(w.WorkEndAt),
		VerifyBeginAt:      optTimePtr(w.VerifyBeginAt),
		VerifyEndAt:        optTimePtr(w.VerifyEndAt),
		DoneAt:             optTimePtr(w.DoneAt),
		BackgroundPID:      w.BackgroundPID,
		AgentLogPath:       w.AgentLogPath,
		LinearIssue:        w.LinearIssue,
	}
}
