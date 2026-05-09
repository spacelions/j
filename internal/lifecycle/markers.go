package lifecycle

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
)

const (
	phasePlan   = "plan"
	phaseWork   = "work"
	phaseVerify = "verify"
)

// Init registers the markers hook that writes one human-readable
// line per status transition to the per-task agent.log. Callers
// should invoke Init once at process startup (e.g. from root.go).
// The hook is a pure observer — it never mutates state or fails
// the transition.
func Init() {
	tasks.Register(markersHook)
}

func markersHook(tr tasks.Transition, task tasks.Task) {
	if task.AgentLogPath == "" {
		return
	}
	f, err := os.OpenFile(task.AgentLogPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() {
		// Best-effort logging: a Close error must not affect the
		// transition the hook is observing.
		_ = f.Close()
	}()

	phase, verb := eventToPhaseVerb(tr.Event)
	ts := time.Now().UTC().Format(time.RFC3339)

	var detail string
	if strings.Contains(string(tr.Event), "begin") ||
		strings.Contains(string(tr.Event), "restart") ||
		strings.Contains(string(tr.Event), "resume") {
		tool, model := task.DisplayToolModel()
		if tool != "" || model != "" {
			detail = fmt.Sprintf("(%s, %s)", tool, model)
		}
	}
	if tr.Event == tasks.EventWorkDone && task.PullRequestURL != "" {
		detail = "pull request: " + task.PullRequestURL
	}

	line := fmt.Sprintf("%s  %s %s", ts, phase, verb)
	if detail != "" {
		line += " — " + detail
	}
	_, _ = fmt.Fprintln(f, line)
}

//nolint:gocyclo,funlen // one case per FSM event; complexity is inherent
func eventToPhaseVerb(e tasks.Event) (phase, verb string) {
	switch e {
	case tasks.EventPlanBegin:
		return phasePlan, "begin"
	case tasks.EventPlanRestart:
		return phasePlan, "restart"
	case tasks.EventPlanDone:
		return phasePlan, "done"
	case tasks.EventPlanAwaitApproval:
		return phasePlan, "await approval"
	case tasks.EventPlanApprove:
		return phasePlan, "approve"
	case tasks.EventPlanQuit:
		return phasePlan, "quit"
	case tasks.EventPlanError:
		return phasePlan, "error"
	case tasks.EventPlanNeedsClarification:
		return phasePlan, "needs clarification"
	case tasks.EventPlanResume:
		return phasePlan, "resume"
	case tasks.EventReaperPlanDone:
		return phasePlan, "done"
	case tasks.EventReaperPlanAwaitApproval:
		return phasePlan, "await approval"
	case tasks.EventReaperPlanFail:
		return phasePlan, "fail"
	case tasks.EventReaperPlanNeedsClarification:
		return phasePlan, "needs clarification"

	case tasks.EventWorkBegin:
		return phaseWork, "begin"
	case tasks.EventWorkRestart:
		return phaseWork, "restart"
	case tasks.EventWorkResume:
		return phaseWork, "resume"
	case tasks.EventWorkDone:
		return phaseWork, "done"
	case tasks.EventWorkQuit:
		return phaseWork, "quit"
	case tasks.EventWorkError:
		return phaseWork, "error"
	case tasks.EventWorkNeedsClarification:
		return phaseWork, "needs clarification"
	case tasks.EventReaperWorkDone:
		return phaseWork, "done"
	case tasks.EventReaperWorkNeedsClarification:
		return phaseWork, "needs clarification"

	case tasks.EventVerifyBegin:
		return phaseVerify, "begin"
	case tasks.EventVerifyRestart:
		return phaseVerify, "restart"
	case tasks.EventVerifyResume:
		return phaseVerify, "resume"
	case tasks.EventVerifyPass:
		return phaseVerify, "pass"
	case tasks.EventVerifyFail:
		return phaseVerify, "fail"
	case tasks.EventVerifyQuit:
		return phaseVerify, "quit"
	case tasks.EventVerifyError:
		return phaseVerify, "error"
	case tasks.EventVerifyStuck:
		return phaseVerify, "stuck"
	case tasks.EventVerifyNeedsClarification:
		return phaseVerify, "needs clarification"
	case tasks.EventReaperVerifyNeedsClarification:
		return phaseVerify, "needs clarification"
	}
	return string(e), ""
}
