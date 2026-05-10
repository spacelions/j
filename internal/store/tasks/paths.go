package tasks

// TaskPaths groups the canonical per-task artifact paths.
type TaskPaths struct {
	Requirements  string
	Plan          string
	VerifierPlan  string
	Findings      string
	Clarification string
}
