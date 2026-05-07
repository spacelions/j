package tasks

import (
	"io"
	"time"
)

// ApplyAndPersist routes a transition through the canonical
// Apply → mutate → PutTask → Notify path. The destination status is
// computed from t.Status, applied to *t (along with StampTerminal for
// terminal outcomes), persisted via s, and finally surfaced to every
// registered hook. The returned Transition records the attempted edge
// regardless of outcome so callers can branch on it for logging.
//
// Notify fires only after PutTask succeeds so observers always see
// durable state. An IllegalTransitionError leaves *t and the store
// untouched; a persist error leaves *t mutated (the caller's snapshot
// reflects the would-be terminal state) so reaper-style flows can
// still surface the in-memory transition to the user even when the
// row could not be written.
func ApplyAndPersist(s *Store, t *Task, ev Event) (Transition, error) {
	from := t.Status
	newStatus, err := Apply(from, ev)
	if err != nil {
		return Transition{From: from, Event: ev}, err
	}
	t.Status = newStatus
	StampTerminal(t)
	tr := Transition{From: from, Event: ev, To: newStatus}
	if err := s.PutTask(*t); err != nil {
		return tr, err
	}
	Notify(tr, *t)
	return tr, nil
}

// ApplyAndPersistWarn mirrors ApplyAndPersist for best-effort
// callers that hold no *Store and use PersistWarn (open-write-close
// per call, stderr warn on failure). Returns the Transition + the
// Apply error so user-facing dispatchers can produce a friendly
// message on illegal transitions; PersistWarn errors stay
// best-effort and are NOT returned, matching the existing contract
// of every begin/finish lifecycle helper. Notify fires only when
// PersistWarn would have written the row, i.e. unconditionally
// after the in-memory mutation — PersistWarn itself swallows IO
// errors so observers may see a transition that did not reach
// disk; that matches the pre-migration behaviour of every caller
// being migrated and is the explicit best-effort contract.
func ApplyAndPersistWarn(
	stderr io.Writer, t *Task, ev Event,
) (Transition, error) {
	from := t.Status
	newStatus, err := Apply(from, ev)
	if err != nil {
		return Transition{From: from, Event: ev}, err
	}
	t.Status = newStatus
	StampTerminal(t)
	tr := Transition{From: from, Event: ev, To: newStatus}
	PersistWarn(stderr, *t)
	Notify(tr, *t)
	return tr, nil
}

// StampTerminal stamps t.DoneAt when t.Status is `completed`.
// `failed` is intentionally excluded: DoneAt records a successful
// completion so `j tasks` can distinguish "ran to a successful end"
// from "ran to a terminal failure".
func StampTerminal(t *Task) {
	if t.Status == StatusCompleted {
		t.DoneAt = time.Now().UTC()
	}
}
