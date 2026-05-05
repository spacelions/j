Prerequisites:
  - From the worktree root.

Steps:
  - Run `make test` and capture the exit code.
  - Run `make coverage` and capture the exit code.

Expected:
  - `make test` exits 0; every package under `./...` reports `ok` and no
    test failures are present (the `internal/cli/tasks` suite contains
    one occasionally-flaky case — `TestRunContinue_PlanDoneSpawnsOrchestrator`
    — that passes on a focused re-run of the test alone).
  - `make coverage` exits 0; the project's allowlist-based gate (every
    non-allowlisted symbol must be at 100% line coverage) passes.
  - Coverage of the new `internal/linear` package is reported (≥80%);
    the new tests for `client.ListProjects`, `picker.PickLinearProject`,
    `tasks.PutTask` linear_issue round-trip, and start-from-Linear all
    appear in the test output.
