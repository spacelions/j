package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestSPA94ResumeWorkFiltersUnchanged pins acceptance criteria AC2 +
// AC5: the resume-work picker filter keys on WorkResumeSession (so a
// task whose backend has minted its resume id mid-run is offered
// regardless of whether the worker process is still alive), while the
// resume-plan and resume-verify filters continue to key on their own
// per-phase fields. A regression here would either hide a running
// codex/deepseek worker from `j tasks resume-work` (AC2) or start
// surfacing planning/verifying rows under resume-work (AC5).
//
// Black-box: the production filter is `t.WorkResumeSession != ""`,
// `t.PlanResumeSession != ""`, `t.VerifyResumeSession != ""`. This
// test pins those three projections against fixture rows so that any
// future cross-wiring is caught.
func TestSPA94ResumeWorkFiltersUnchanged(t *testing.T) {
	rows := []tasks.Task{
		{
			ID:                "work-only",
			Status:            tasks.StatusWorking,
			WorkResumeSession: "work-sess",
		},
		{
			ID:                "plan-only",
			Status:            tasks.StatusPlanning,
			PlanResumeSession: "plan-sess",
		},
		{
			ID:                  "verify-only",
			Status:              tasks.StatusVerifying,
			VerifyResumeSession: "verify-sess",
		},
		{ID: "no-session", Status: tasks.StatusWorking},
	}

	work := filterSPA94(rows, func(r tasks.Task) bool {
		return r.WorkResumeSession != ""
	})
	if len(work) != 1 || work[0].ID != "work-only" {
		t.Fatalf("resume-work filter = %v, want [work-only]", idsSPA94(work))
	}

	plan := filterSPA94(rows, func(r tasks.Task) bool {
		return r.PlanResumeSession != ""
	})
	if len(plan) != 1 || plan[0].ID != "plan-only" {
		t.Fatalf("resume-plan filter = %v, want [plan-only]", idsSPA94(plan))
	}

	verify := filterSPA94(rows, func(r tasks.Task) bool {
		return r.VerifyResumeSession != ""
	})
	if len(verify) != 1 || verify[0].ID != "verify-only" {
		t.Fatalf(
			"resume-verify filter = %v, want [verify-only]",
			idsSPA94(verify),
		)
	}
}

func filterSPA94(
	rows []tasks.Task, keep func(tasks.Task) bool,
) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func idsSPA94(rows []tasks.Task) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
