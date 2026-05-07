package lifecycle

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
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
	defer f.Close()

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

func eventToPhaseVerb(e tasks.Event) (phase, verb string) {
	switch e {
	case tasks.EventPlanBegin:
		return "plan", "begin"
	case tasks.EventPlanRestart:
		return "plan", "restart"
	case tasks.EventPlanDone:
		return "plan", "done"
	case tasks.EventPlanAwaitApproval:
		return "plan", "await approval"
	case tasks.EventPlanApprove:
		return "plan", "approve"
	case tasks.EventPlanQuit:
		return "plan", "quit"
	case tasks.EventPlanError:
		return "plan", "error"
	case tasks.EventPlanResume:
		return "plan", "resume"
	case tasks.EventReaperPlanDone:
		return "plan", "done"
	case tasks.EventReaperPlanAwaitApproval:
		return "plan", "await approval"
	case tasks.EventReaperPlanFail:
		return "plan", "fail"
	case tasks.EventReaperPlanNeedsClarification:
		return "plan", "needs clarification"

	case tasks.EventWorkBegin:
		return "work", "begin"
	case tasks.EventWorkRestart:
		return "work", "restart"
	case tasks.EventWorkResume:
		return "work", "resume"
	case tasks.EventWorkDone:
		return "work", "done"
	case tasks.EventWorkQuit:
		return "work", "quit"
	case tasks.EventWorkError:
		return "work", "error"
	case tasks.EventReaperWorkDone:
		return "work", "done"
	case tasks.EventReaperWorkNeedsClarification:
		return "work", "needs clarification"

	case tasks.EventVerifyBegin:
		return "verify", "begin"
	case tasks.EventVerifyRestart:
		return "verify", "restart"
	case tasks.EventVerifyResume:
		return "verify", "resume"
	case tasks.EventVerifyPass:
		return "verify", "pass"
	case tasks.EventVerifyFail:
		return "verify", "fail"
	case tasks.EventVerifyQuit:
		return "verify", "quit"
	case tasks.EventVerifyError:
		return "verify", "error"
	case tasks.EventVerifyStuck:
		return "verify", "stuck"
	case tasks.EventReaperVerifyNeedsClarification:
		return "verify", "needs clarification"
	}
	return string(e), ""
}
