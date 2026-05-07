package testcases_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// recoveryFakeUI satisfies UI / RePlanUI / ReVerifyUI for the
// `re-*` and `resume-*` black-box tests below: it returns a fixed
// task id from PickTask and accepts every status-override prompt.
type recoveryFakeUI struct {
	pickReturn string
	statusOK   bool
}

func (u *recoveryFakeUI) ConfirmDiscard(
	context.Context, tasks.Task,
) (bool, error) {
	return false, nil
}

func (u *recoveryFakeUI) PickTask(
	_ context.Context, _ []tasks.Task,
) (string, bool, error) {
	if u.pickReturn == "" {
		return "", false, nil
	}
	return u.pickReturn, true, nil
}

func (u *recoveryFakeUI) ConfirmStatusOverride(
	_ context.Context, _, _, _ string,
) (bool, error) {
	return u.statusOK, nil
}

// recoveryArgvJStub writes a tiny shell script that records its argv
// to outputPath, one argument per line. The script writes to a `.tmp`
// sibling and renames atomically so a polling reader never sees a
// partial argv list.
func recoveryArgvJStub(t *testing.T, outputPath string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-recovery-stub.sh")
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q.tmp && mv %q.tmp %q\n",
		outputPath, outputPath, outputPath,
	)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// recoveryReadStubArgv polls outputPath for the argv list written by
// recoveryArgvJStub.
func recoveryReadStubArgv(t *testing.T, outputPath string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outputPath)
		if err == nil && len(data) > 0 {
			return strings.Split(
				strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("argv stub never wrote to %s", outputPath)
	return nil
}

// recoverySetupEnv chdirs into a fresh tempdir, primes the store and
// the planner / worker / verifier agent buckets so the per-command
// preflight skips its prompt path.
func recoverySetupEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	for _, bucket := range []string{
		store.BucketPlanner,
		store.BucketWorker,
		store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(
			t, bucket, "cursor", "sonnet-4")
	}
}

// recoverySeedTask writes a minimal task row and the on-disk
// requirements.md / plan.md artefacts the recovery commands assume.
func recoverySeedTask(
	t *testing.T, mutate func(*tasks.Task),
) string {
	t.Helper()
	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.RequirementsFileName),
		[]byte("# req\nbody"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.PlanFileName),
		[]byte("1. step\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	begin := time.Now().UTC().Add(-2 * time.Hour)
	end := begin.Add(time.Hour)
	row := tasks.Task{
		ID:          id,
		Status:      tasks.StatusCompleted,
		PlanTool:    "cursor",
		PlanModel:   "sonnet-4",
		WorkTool:    "cursor",
		WorkModel:   "sonnet-4",
		VerifyTool:  "cursor",
		VerifyModel: "sonnet-4",
		Summary:     "recovery e2e test row",
		PlanBeginAt: begin,
		PlanEndAt:   end,
	}
	if mutate != nil {
		mutate(&row)
	}
	testutil.SeedTaskRow(t, row)
	return id
}
