package testcases_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_Replan_AppendsNewComment pins the "re-planning the
// same task adds a new comment rather than editing the previous one"
// acceptance criterion. The description is overwritten on every plan
// finish, but each commentCreate posts a fresh comment so plan
// history is preserved upstream.
func TestLinearPush_Replan_AppendsNewComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newLinearPushEnv(t, id, "req v1", "plan v1")
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearPush()

	firePlanDone(id, "ENG-1", tasks.EventPlanDone)

	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	writeArtefact(t,
		filepath.Join(dir, id, tasks.RequirementsFileName), "req v2")
	writeArtefact(t,
		filepath.Join(dir, id, tasks.PlanFileName), "plan v2")
	firePlanDone(id, "ENG-1", tasks.EventReaperPlanDone)

	got := env.recordedBodies()
	comments := 0
	updates := 0
	for _, body := range got {
		if strings.Contains(body, "commentCreate") {
			comments++
		}
		if strings.Contains(body, "issueUpdate") {
			updates++
		}
	}
	if comments != 2 {
		t.Fatalf("want 2 commentCreate calls across replans, got %d in %v",
			comments, got)
	}
	if updates != 2 {
		t.Fatalf("want 2 issueUpdate calls across replans, got %d in %v",
			updates, got)
	}
}
