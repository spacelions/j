package orchestrator

import "fmt"

// PhaseOverrides carries one-off flag overrides for whichever phase
// the orchestrator is going to run. Zero value = no override (existing
// callers pass zero struct).
//
// Tool / Model / Yes are planner-specific; Interactive flows into the
// active phase (planner when not skipped, otherwise worker). Resume
// state is intentionally not part of this struct: the worker / verifier
// infer it from the task row's WorkResumeSession / VerifyResumeSession
// fields. Single-phase resume commands preserve them so a phase can
// reuse its prior agent session when one exists.
type PhaseOverrides struct {
	Tool        string
	Model       string
	Interactive bool
	Yes         bool
}

// RunPhase selects the slice of the planner→worker→verifier chain a
// single RunForTask invocation drives. Encoded as a string so it
// round-trips cleanly through cobra (`--phase=...`) / viper / agent
// log markers; expressing the previous bool-pair encoding's
// impossible combination is unrepresentable.
type RunPhase string

const (
	// RunPhaseFull runs planner → worker → verifier. Used by
	// `j tasks start` and `j tasks continue` on a fresh row.
	RunPhaseFull RunPhase = "full"
	// RunPhasePlanOnly runs only the planner.
	RunPhasePlanOnly RunPhase = "plan-only"
	// RunPhaseFromWork skips the planner and runs worker → verifier.
	// Used by `j tasks continue` on a plan-done row.
	RunPhaseFromWork RunPhase = "from-work"
	// RunPhaseWorkOnly runs only the worker.
	RunPhaseWorkOnly RunPhase = "work-only"
	// RunPhaseVerifyOnly runs only the verifier.
	RunPhaseVerifyOnly RunPhase = "verify-only"
)

// ParseRunPhase resolves a string to a RunPhase. Empty maps to
// RunPhaseFull so a missing flag value behaves like the default. Any
// other unknown value is rejected so a typo at the CLI surfaces
// instead of silently running the planner.
func ParseRunPhase(s string) (RunPhase, error) {
	switch s {
	case "", string(RunPhaseFull):
		return RunPhaseFull, nil
	case string(RunPhasePlanOnly):
		return RunPhasePlanOnly, nil
	case string(RunPhaseFromWork):
		return RunPhaseFromWork, nil
	case string(RunPhaseWorkOnly):
		return RunPhaseWorkOnly, nil
	case string(RunPhaseVerifyOnly):
		return RunPhaseVerifyOnly, nil
	}
	return "", fmt.Errorf(
		"workflow: unknown run phase %q "+
			"(want full|plan-only|from-work|work-only|verify-only)", s)
}
