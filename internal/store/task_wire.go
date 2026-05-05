package store

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
//
// All other fields mirror Task's on-disk shape (snake_case keys,
// matching the JSON tags so external tooling that reads the row
// either via the file or via an agentlog JSON marker sees identical
// names).
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
}

func taskToWire(t Task) taskWire {
	w := taskWire{
		ID:                 t.ID,
		Status:             t.Status,
		InvokedTool:        t.InvokedTool,
		InvokedModel:       t.InvokedModel,
		Worktree:           t.Worktree,
		Summary:            t.Summary,
		PlanResumeCursor:   t.PlanResumeCursor,
		WorkResumeCursor:   t.WorkResumeCursor,
		VerifyResumeCursor: t.VerifyResumeCursor,
		BackgroundPID:      t.BackgroundPID,
		AgentLogPath:       t.AgentLogPath,
	}
	if t.PlanBeginAt != nil {
		w.PlanBeginAt = *t.PlanBeginAt
	}
	if t.PlanEndAt != nil {
		w.PlanEndAt = *t.PlanEndAt
	}
	if t.WorkBeginAt != nil {
		w.WorkBeginAt = *t.WorkBeginAt
	}
	if t.WorkEndAt != nil {
		w.WorkEndAt = *t.WorkEndAt
	}
	if t.VerifyBeginAt != nil {
		w.VerifyBeginAt = *t.VerifyBeginAt
	}
	if t.VerifyEndAt != nil {
		w.VerifyEndAt = *t.VerifyEndAt
	}
	if t.DoneAt != nil {
		w.DoneAt = *t.DoneAt
	}
	return w
}

func wireToTask(w taskWire) Task {
	t := Task{
		ID:                 w.ID,
		Status:             w.Status,
		InvokedTool:        w.InvokedTool,
		InvokedModel:       w.InvokedModel,
		Worktree:           w.Worktree,
		Summary:            w.Summary,
		PlanResumeCursor:   w.PlanResumeCursor,
		WorkResumeCursor:   w.WorkResumeCursor,
		VerifyResumeCursor: w.VerifyResumeCursor,
		BackgroundPID:      w.BackgroundPID,
		AgentLogPath:       w.AgentLogPath,
	}
	if !w.PlanBeginAt.IsZero() {
		v := w.PlanBeginAt
		t.PlanBeginAt = &v
	}
	if !w.PlanEndAt.IsZero() {
		v := w.PlanEndAt
		t.PlanEndAt = &v
	}
	if !w.WorkBeginAt.IsZero() {
		v := w.WorkBeginAt
		t.WorkBeginAt = &v
	}
	if !w.WorkEndAt.IsZero() {
		v := w.WorkEndAt
		t.WorkEndAt = &v
	}
	if !w.VerifyBeginAt.IsZero() {
		v := w.VerifyBeginAt
		t.VerifyBeginAt = &v
	}
	if !w.VerifyEndAt.IsZero() {
		v := w.VerifyEndAt
		t.VerifyEndAt = &v
	}
	if !w.DoneAt.IsZero() {
		v := w.DoneAt
		t.DoneAt = &v
	}
	return t
}
